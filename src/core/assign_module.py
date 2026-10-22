"""Приписывание брутто-формул по параметрам, пришедшим из веб-формы."""
from __future__ import annotations

import json
import logging
from dataclasses import dataclass, field
from typing import Dict, Mapping, Optional, Tuple

import HumSpectra.mass_spectra.assign.assign as assign
import HumSpectra.mass_spectra.calibration.calibration as calibration
import HumSpectra.mass_spectra.tmds.tmds as tmds
import HumSpectra.mass_spectra.utilits.utilits as mut

log = logging.getLogger(__name__)

BruttoDict = Dict[str, Tuple[int, int]]

# Значения по умолчанию — те, что раньше были зашиты в код
DEFAULT_BRUTTO_DICT: BruttoDict = {
    'C': (4, 50), 'H': (4, 80), 'O': (0, 50),
    'N': (0, 3), 'C_13': (0, 1), 'S': (0, 3),
}
DEFAULT_TMDS_BRUTTO_DICT: BruttoDict = {
    'C': (-1, 20), 'H': (-4, 40), 'O': (-1, 20), 'N': (0, 1),
}
DEFAULT_REL_ERROR = 0.5  # ppm
DEFAULT_CHARGE_MAX = 1

PROTOCOLS = ('non_tmds', 'tmds', 'custom')


@dataclass
class AssignParams:
    """Параметры приписывания. Собираются из gRPC-запроса в from_request()."""
    protocol: str = 'non_tmds'
    brutto_dict: BruttoDict = field(default_factory=lambda: dict(DEFAULT_BRUTTO_DICT))
    rel_error: float = DEFAULT_REL_ERROR
    charge_max: int = DEFAULT_CHARGE_MAX
    sulfur_precision_factor: float = 10.0
    nitrogen_precision_factor: float = 4.0
    recalibrate: bool = True
    # только для протокола tmds
    tmds_brutto_dict: BruttoDict = field(default_factory=lambda: dict(DEFAULT_TMDS_BRUTTO_DICT))
    tmds_rel_error: float = 0.5
    tmds_max_num: int = 100

    @classmethod
    def from_request(cls, request) -> "AssignParams":
        """Читает MassListRequest. Незаданные (нулевые) поля → значения по умолчанию."""
        p = cls()

        protocol = (request.protocole or 'non_tmds').strip().lower()
        if protocol not in PROTOCOLS:
            raise ValueError(f"Unknown protocol {protocol!r}, expected one of {PROTOCOLS}")
        p.protocol = protocol

        if len(request.brutto_dict) > 0:
            p.brutto_dict = {el: (int(r.min), int(r.max)) for el, r in request.brutto_dict.items()}
        if request.rel_error > 0:
            p.rel_error = float(request.rel_error)
        if request.charge_max > 0:
            p.charge_max = int(request.charge_max)

        opts: Mapping[str, str] = request.options
        p.sulfur_precision_factor = _opt_float(opts, 'sulfur_precision_factor', p.sulfur_precision_factor)
        p.nitrogen_precision_factor = _opt_float(opts, 'nitrogen_precision_factor', p.nitrogen_precision_factor)
        p.recalibrate = _opt_bool(opts, 'recalibrate', p.recalibrate)
        p.tmds_rel_error = _opt_float(opts, 'tmds_rel_error', p.tmds_rel_error)
        p.tmds_max_num = int(_opt_float(opts, 'tmds_max_num', p.tmds_max_num))
        if opts.get('tmds_brutto_dict'):
            p.tmds_brutto_dict = _parse_brutto_json(opts['tmds_brutto_dict'])

        for el, (lo, hi) in p.brutto_dict.items():
            if lo > hi:
                raise ValueError(f"brutto_dict: {el} min ({lo}) > max ({hi})")
        return p


def _opt_float(opts: Mapping[str, str], key: str, default: float) -> float:
    raw = opts.get(key, '')
    if raw == '':
        return default
    try:
        return float(raw)
    except ValueError as e:
        raise ValueError(f"option {key}: expected number, got {raw!r}") from e


def _opt_bool(opts: Mapping[str, str], key: str, default: bool) -> bool:
    raw = opts.get(key, '').strip().lower()
    if raw == '':
        return default
    if raw in ('1', 'true', 'yes', 'on'):
        return True
    if raw in ('0', 'false', 'no', 'off'):
        return False
    raise ValueError(f"option {key}: expected true/false, got {raw!r}")


def _parse_brutto_json(raw: str) -> BruttoDict:
    """'{"C": [-1, 20]}' или '{"C": {"min": -1, "max": 20}}' → {'C': (-1, 20)}"""
    data = json.loads(raw)
    out: BruttoDict = {}
    for el, v in data.items():
        if isinstance(v, dict):
            out[el] = (int(v['min']), int(v['max']))
        else:
            lo, hi = v
            out[el] = (int(lo), int(hi))
    return out


def _assign(ms_list, params: AssignParams, rel_error: Optional[float] = None):
    return assign.assign_optimized(
        ms_list,
        brutto_dict=params.brutto_dict,
        rel_error=params.rel_error if rel_error is None else rel_error,
        sulfur_precision_factor=params.sulfur_precision_factor,
        nitrogen_precision_factor=params.nitrogen_precision_factor,
        charge_max=params.charge_max,
    )


def _keep_organic(spectra):
    """Оставляем только формулы с C, H и O"""
    return spectra.loc[(spectra['C'] != 0) & (spectra['H'] != 0) & (spectra['O'] != 0)]


def process(ms_list, name: str, params: AssignParams):
    """Точка входа: выбирает пайплайн по протоколу."""
    log.info("Processing %s: protocol=%s rel_error=%s charge_max=%s brutto=%s",
             name, params.protocol, params.rel_error, params.charge_max, params.brutto_dict)

    ms_list.attrs['name'] = name
    if params.recalibrate:
        ms_list = calibration.recallibrate_optimize(ms_list, draw=False)

    if params.protocol == 'tmds':
        spectra = _process_tmds(ms_list, params)
    else:
        # 'custom' пока использует тот же пайплайн, что и non_tmds,
        # но целиком управляется параметрами из формы
        spectra = _keep_organic(_assign(ms_list, params))

    spectra.attrs['name'] = name
    return spectra


def process_non_tmds(ms_list, name, params: Optional[AssignParams] = None):
    """Совместимость со старым вызовом."""
    return process(ms_list, name, params or AssignParams())


def _process_tmds(ms_list, params: AssignParams):
    """TMDS: приписывание → разности масс → доприписывание по TMDS
    (повторяет process_tmds из process_raw_mass_data.py)."""
    spectra = _assign(ms_list, params)

    spectra = mut.calc_mass(spectra)
    tmds_spectra = tmds.calc_by_brutto(spectra)
    tmds_spectra = assign.assign_optimized(tmds_spectra, brutto_dict=params.tmds_brutto_dict)
    tmds_spectra = mut.calc_mass(tmds_spectra, debug=False)

    spectra = tmds.assign_by_tmds_optimize(
        spectra, tmds_spectra,
        rel_error=params.tmds_rel_error,
        max_num=params.tmds_max_num,
    )
    return _keep_organic(spectra)
