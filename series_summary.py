"""
Сводная статистика по серии обработанных масс-спектров (mzlist после приписывания формул).

Использование:
    summary = series_summary(list_of_dataframes)            # список DataFrame
    summary = series_summary(Path(r'D:\\lab\\karina\\results\\mzlist'))  # папка с *__mzlist.csv
    summary.to_excel(document_save_path / 'series_summary.xlsx')
"""
from pathlib import Path
from typing import Iterable, Union

import numpy as np
import pandas as pd

# Столбцы, которые не имеет смысла усреднять
_SKIP_COLS = {'assign', 'charge', 'CRAM', 'abs_error', 'rel_error', 'intensity'}
_ELEMENTS = ['C', 'H', 'O', 'N', 'S', 'P', 'C_13']


def _wavg(x: pd.Series, w: pd.Series) -> float:
    m = x.notna() & w.notna()
    return float(np.average(x[m], weights=w[m])) if w[m].sum() > 0 else np.nan


def _formula_group(df: pd.DataFrame) -> pd.Series:
    """CHO / CHON / CHOS / CHONS (+P) по наличию гетероатомов."""
    g = pd.Series('CHO', index=df.index)
    has = lambda el: (df[el] > 0) if el in df.columns else pd.Series(False, index=df.index)
    n, s, p = has('N'), has('S'), has('P')
    g = g.where(~n, g + 'N')
    g = g.where(~s, g + 'S')
    g = g.where(~p, g + 'P')
    return g


def _error_stats(df: pd.DataFrame) -> dict:
    e = df['rel_error'].dropna()          # ppm
    w = df.loc[e.index, 'intensity'] if 'intensity' in df.columns else None
    ae = e.abs()
    res = {
        'err_mean_ppm': e.mean(),                       # систематический сдвиг калибровки
        'err_median_ppm': e.median(),
        'err_std_ppm': e.std(),
        'err_RMSE_ppm': np.sqrt((e ** 2).mean()),
        'err_MAE_ppm': ae.mean(),
        'err_median_abs_ppm': ae.median(),
        'err_MAD_ppm': (e - e.median()).abs().median(),  # робастный разброс
        'err_p95_abs_ppm': ae.quantile(0.95),
        'err_max_abs_ppm': ae.max(),
        'err_min_ppm': e.min(),
        'err_max_ppm': e.max(),
        'frac_err_le_0.2ppm_%': (ae <= 0.2).mean() * 100,
        'frac_err_le_0.5ppm_%': (ae <= 0.5).mean() * 100,
        'frac_err_le_1ppm_%': (ae <= 1.0).mean() * 100,
    }
    if w is not None:
        res['err_RMSE_w_ppm'] = np.sqrt(_wavg(e ** 2, w))   # взвешенная по интенсивности
        res['err_mean_w_ppm'] = _wavg(e, w)
    if 'abs_error' in df.columns:
        a = df['abs_error'].dropna()
        res['err_RMSE_mDa'] = np.sqrt((a ** 2).mean()) * 1e3
    # Тренд ошибки по массе (ppm на 100 Да) — показывает некорректную калибровку
    if len(e) > 2 and 'mass' in df.columns:
        res['err_slope_ppm_per_100Da'] = np.polyfit(df.loc[e.index, 'mass'], e, 1)[0] * 100
    return res


def spectrum_summary(df: pd.DataFrame, name: str | None = None) -> dict:
    """Одна строка сводной таблицы для одного спектра."""
    df = df.copy()
    if 'assign' in df.columns:
        df = df[df['assign'].astype(bool)]
    name = name or df.attrs.get('name', 'spectrum')
    w = df['intensity'] if 'intensity' in df.columns else pd.Series(1.0, index=df.index)

    row = {'name': name, 'n_formulas': len(df)}
    if 'brutto' in df.columns:
        row['n_unique_brutto'] = df['brutto'].nunique()
    row['total_intensity'] = w.sum()

    # Диапазон масс и молекулярно-массовое распределение
    if 'calc_mass' in df.columns or 'mass' in df.columns:
        m = df['calc_mass'] if 'calc_mass' in df.columns else df['mass']
        row['mass_min'] = df['mass'].min() if 'mass' in df.columns else m.min()
        row['mass_max'] = df['mass'].max() if 'mass' in df.columns else m.max()
        row['Mn'] = _wavg(m, w)                          # среднечисловая (по интенсивности)
        row['Mw'] = _wavg(m ** 2, w) / row['Mn']         # среднемассовая
        row['PDI'] = row['Mw'] / row['Mn']

    # Группы формул по гетероатомам (по числу и по интенсивности)
    grp = _formula_group(df)
    for g in ['CHO', 'CHON', 'CHOS', 'CHONS', 'CHOP', 'CHONP', 'CHOSP', 'CHONSP']:
        mask = grp == g
        if mask.any() or g in ('CHO', 'CHON', 'CHOS', 'CHONS'):
            row[f'{g}_n'] = int(mask.sum())
            row[f'{g}_%'] = mask.mean() * 100
            row[f'{g}_%int'] = w[mask].sum() / w.sum() * 100
    if 'C_13' in df.columns:
        row['with_13C_%'] = (df['C_13'] > 0).mean() * 100

    # Средние: простое и взвешенное по интенсивности для всех числовых столбцов
    num_cols = [c for c in df.select_dtypes(include=np.number).columns if c not in _SKIP_COLS]
    num_cols = sorted(num_cols, key=lambda c: (_ELEMENTS.index(c) if c in _ELEMENTS else 99))
    for c in num_cols:
        row[f'{c}_mean'] = df[c].mean()
    for c in num_cols:
        row[f'{c}_wmean'] = _wavg(df[c], w)

    # Классы соединений
    if 'class' in df.columns:
        for cl in sorted(df['class'].dropna().unique()):
            mask = df['class'] == cl
            row[f'class_{cl}_%'] = mask.mean() * 100
            row[f'class_{cl}_%int'] = w[mask].sum() / w.sum() * 100
    if 'CRAM' in df.columns:
        cram = df['CRAM'].astype(bool)
        row['CRAM_%'] = cram.mean() * 100
        row['CRAM_%int'] = w[cram].sum() / w.sum() * 100

    # Ароматичность по AI (Koch & Dittmar)
    if 'AI' in df.columns:
        row['aromatic_AI>0.5_%'] = (df['AI'] > 0.5).mean() * 100
        row['condensed_AI>=0.67_%'] = (df['AI'] >= 0.67).mean() * 100
        row['aromatic_AI>0.5_%int'] = w[df['AI'] > 0.5].sum() / w.sum() * 100

    # Ошибки приписывания
    if 'rel_error' in df.columns:
        row.update(_error_stats(df))
    return row


def series_summary(spectra: Union[Iterable[pd.DataFrame], str, Path],
                   pattern: str = '*__mzlist.csv',
                   add_series_stats: bool = True,
                   transpose: bool = False) -> pd.DataFrame:
    """
    Сводная таблица по серии спектров: одна строка на спектр
    (+ строки mean / std / RSD% / min / max по всей серии).

    spectra  : список DataFrame (имя берётся из df.attrs['name']) или папка с mzlist-файлами
    pattern  : маска файлов при передаче папки
    transpose: True — показатели строками, спектры столбцами (удобно читать в Excel)
    """
    rows = []
    if isinstance(spectra, (str, Path)):
        for f in sorted(Path(spectra).rglob(pattern)):
            rows.append(spectrum_summary(pd.read_csv(f), name=f.name.replace('__mzlist.csv', '')))
    else:
        for i, df in enumerate(spectra):
            rows.append(spectrum_summary(df, name=df.attrs.get('name', f'spectrum_{i}')))

    table = pd.DataFrame(rows).set_index('name')
    # Порядок столбцов как в первом спектре, новые классы — в конце
    if add_series_stats and len(table) > 1:
        num = table.select_dtypes(include=np.number)
        stats = pd.DataFrame({
            'SERIES_mean': num.mean(),
            'SERIES_std': num.std(),
            'SERIES_RSD_%': num.std() / num.mean().abs() * 100,
            'SERIES_min': num.min(),
            'SERIES_max': num.max(),
        }).T
        table = pd.concat([table, stats])
    return table.T if transpose else table


if __name__ == '__main__':
    import sys
    t = series_summary(Path(sys.argv[1]) if len(sys.argv) > 1 else Path('.'), transpose=True)
    pd.set_option('display.max_rows', 300)
    print(t)
