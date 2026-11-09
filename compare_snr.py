#!/usr/bin/env python3
"""
Сравнение порога S/N центроидирования CWT в msconvert.

Конвертирует один исходный файл (.d / .raw / профильный .mzML) с теми же
фильтрами, что собирает веб-интерфейс (internal/model/convert.go):
    peakPicking cwt snr=<S> peakSpace=<Δ> msLevel=<уровни>
и для каждого S считает пики в MS1-спектрах.

Запуск (в WSL, где работает msconvert):
    python3 compare_snr.py /путь/к/образцу.d
    python3 compare_snr.py образец.d --snr 1 2 3 5 10 20 --space 0.001 --mz 150 1000

Результат: таблица в консоли и snr_compare/<имя>/summary.csv.
Только стандартная библиотека Python.
"""
import argparse
import base64
import csv
import os
import shutil
import statistics
import subprocess
import sys
import time
import xml.etree.ElementTree as ET
import zlib
from array import array
from bisect import bisect_left

# Accession-коды PSI-MS
MS_LEVEL = "MS:1000511"
MZ_ARRAY = "MS:1000514"
INT_ARRAY = "MS:1000515"
F64 = "MS:1000523"
F32 = "MS:1000521"
ZLIB = "MS:1000574"
NUMPRESS = ("MS:1002312", "MS:1002313", "MS:1002314")
CENTROID = "MS:1000127"
PROFILE = "MS:1000128"
PEAK_PICKING = "MS:1000035"


def local(tag):
    return tag.rsplit("}", 1)[-1]


def decode(binary_el, params):
    raw = base64.b64decode(binary_el.text or "")
    if any(a in params for a in NUMPRESS):
        sys.exit("mzML сжат numpress — уберите numpress из параметров msconvert")
    if ZLIB in params:
        raw = zlib.decompress(raw)
    arr = array("d" if F64 in params else "f")
    arr.frombytes(raw)
    if sys.byteorder != "little":
        arr.byteswap()
    return arr


def read_spectra(path, ms_level=1):
    """Возвращает список (mz, intensity) для спектров нужного уровня MS."""
    out = []
    for _, el in ET.iterparse(path, events=("end",)):
        if local(el.tag) != "spectrum":
            continue
        level, rep = None, "?"
        for cv in el:
            if local(cv.tag) != "cvParam":
                continue
            acc = cv.get("accession")
            if acc == MS_LEVEL:
                level = int(cv.get("value"))
            elif acc == CENTROID:
                rep = "centroid"
            elif acc == PROFILE:
                rep = "profile"
        if level == ms_level:
            mz = it = None
            for bda in el.iter():
                if local(bda.tag) != "binaryDataArray":
                    continue
                params = {c.get("accession") for c in bda if local(c.tag) == "cvParam"}
                binary = next(c for c in bda if local(c.tag) == "binary")
                data = decode(binary, params)
                if MZ_ARRAY in params:
                    mz = data
                elif INT_ARRAY in params:
                    it = data
            if mz is not None and it is not None:
                out.append((mz, it, rep))
        el.clear()
    return out


def header_info(path):
    """Есть ли в dataProcessing шаг peak picking и какие методы записаны."""
    head = []
    with open(path, encoding="utf-8", errors="replace") as f:
        for line in f:
            if "<run" in line:
                break
            head.append(line)
    text = "".join(head)
    picked = PEAK_PICKING in text
    methods = sorted({l.split('name="', 1)[1].split('"', 1)[0]
                      for l in head if "<userParam" in l and 'name="' in l})
    return picked, methods


def run_msconvert(exe, src, out_dir, snr, space, levels, mz):
    filters = [f"peakPicking cwt snr={snr:g} peakSpace={space:g} msLevel={levels}"]
    if mz:
        filters.append(f"mzWindow [{mz[0]:g},{mz[1]:g}]")
    cmd = [exe, src, "-o", out_dir, "--mzML"]
    for f in filters:
        cmd += ["--filter", f]
    os.makedirs(out_dir, exist_ok=True)
    t0 = time.time()
    res = subprocess.run(cmd, capture_output=True, text=True)
    if res.returncode != 0:
        sys.stderr.write(res.stdout[-2000:] + res.stderr[-2000:])
        sys.exit(f"msconvert завершился с кодом {res.returncode} (snr={snr:g})")
    files = [f for f in os.listdir(out_dir) if f.lower().endswith(".mzml")]
    if not files:
        sys.exit(f"msconvert не создал .mzML в {out_dir}")
    log = (res.stdout + res.stderr).strip().splitlines()
    return os.path.join(out_dir, files[0]), time.time() - t0, log


def matched_fraction(ref_mz, mz, ppm):
    """Доля пиков mz, у которых есть пара в ref_mz в пределах ppm."""
    if not mz:
        return 0.0
    ref = sorted(ref_mz)
    hit = 0
    for m in mz:
        tol = m * ppm * 1e-6
        i = bisect_left(ref, m - tol)
        if i < len(ref) and ref[i] <= m + tol:
            hit += 1
    return hit / len(mz)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("input", help="исходный файл (.d, .raw или профильный .mzML)")
    ap.add_argument("--snr", type=float, nargs="+", default=[1, 2, 3, 5, 10, 20])
    ap.add_argument("--space", type=float, default=0.001, help="peakSpace, m/z (как в интерфейсе)")
    ap.add_argument("--levels", default="1-", help="msLevel (как в интерфейсе)")
    ap.add_argument("--mz", type=float, nargs=2, metavar=("MIN", "MAX"), help="окно m/z")
    ap.add_argument("--ppm", type=float, default=1.0, help="допуск сопоставления пиков, ppm")
    ap.add_argument("--msconvert", default=os.environ.get("MSCONVERT_PATH") or shutil.which("msconvert") or "msconvert")
    ap.add_argument("--out", default="snr_compare")
    a = ap.parse_args()

    src = os.path.abspath(a.input.rstrip("/"))
    name = os.path.splitext(os.path.basename(src))[0]
    base_dir = os.path.abspath(os.path.join(a.out, name))
    snrs = sorted(set(a.snr))

    rows, ref = [], None
    for snr in snrs:
        out_dir = os.path.join(base_dir, f"snr_{snr:g}")
        print(f"→ snr={snr:g} …", flush=True)
        mzml, secs, log = run_msconvert(a.msconvert, src, out_dir, snr, a.space, a.levels, a.mz)
        spectra = read_spectra(mzml)
        if not spectra:
            sys.exit(f"В {mzml} нет MS1-спектров")
        counts = [len(s[0]) for s in spectra]
        # Основной спектр — с наибольшим числом пиков (для FTICR обычно он единственный)
        mz, it, rep = max(spectra, key=lambda s: len(s[0]))
        picked, methods = header_info(mzml)
        steps = sorted(mz[i + 1] - mz[i] for i in range(len(mz) - 1))
        dmz = steps[len(steps) // 2] if steps else 0.0
        if snr == snrs[0]:
            print(f"   тип спектра: {rep}; peak picking в dataProcessing: {'да' if picked else 'НЕТ'}")
            if methods:
                print("   методы обработки: " + "; ".join(methods))
            print(f"   медианный шаг m/z между соседними точками: {dmz:.6g}")
            for line in log[-6:]:
                print("   msconvert: " + line)
        ints = sorted(it)
        if ref is None:
            ref = list(mz)
        rows.append({
            "snr": f"{snr:g}",
            "scans": len(spectra),
            "peaks_mean_per_scan": round(statistics.mean(counts), 1),
            "peaks_main_scan": len(mz),
            "share_of_lowest_snr_%": round(100 * len(mz) / len(ref), 1) if ref else "",
            "matched_to_lowest_snr_%": round(100 * matched_fraction(ref, mz, a.ppm), 1),
            "min_intensity": f"{ints[0]:.3g}" if ints else "",
            "median_intensity": f"{statistics.median(ints):.3g}" if ints else "",
            "mz_range": f"{min(mz):.2f}–{max(mz):.2f}" if len(mz) else "",
            "spectrum_type": rep,
            "peak_picking_logged": "yes" if picked else "no",
            "median_step_mz": f"{dmz:.6g}",
            "convert_s": round(secs, 1),
        })

    os.makedirs(base_dir, exist_ok=True)
    csv_path = os.path.join(base_dir, "summary.csv")
    with open(csv_path, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0]))
        w.writeheader()
        w.writerows(rows)

    head = ["snr", "пиков", "% от snr=" + rows[0]["snr"], "совпало %", "мин. I", "медиана I", "m/z"]
    keys = ["snr", "peaks_main_scan", "share_of_lowest_snr_%", "matched_to_lowest_snr_%",
            "min_intensity", "median_intensity", "mz_range"]
    table = [head] + [[str(r[k]) for k in keys] for r in rows]
    widths = [max(len(row[i]) for row in table) for i in range(len(head))]
    print()
    for row in table:
        print("  ".join(c.rjust(w) for c, w in zip(row, widths)))
    if len({r["peaks_main_scan"] for r in rows}) == 1 and len(rows) > 1:
        print("\n⚠ Число пиков не зависит от snr — фильтр CWT, по-видимому, не сработал.")
        if rows[0]["spectrum_type"] == "centroid" and rows[0]["peak_picking_logged"] == "no":
            print("  Спектры уже помечены как центроидные: CWT их пропускает. Нужен профильный спектр прибора.")
        elif rows[0]["spectrum_type"] == "profile":
            print("  В выходном файле профильный спектр: центроидирование не выполнено. См. вывод msconvert выше.")
    print(f"\nСканов MS1: {rows[0]['scans']}. Таблица сохранена: {csv_path}")
    print(f"«совпало %» — доля пиков, найденных и при наименьшем snr (±{a.ppm:g} ppm):"
          " ~100% значит, что рост S/N только отбрасывает слабые пики, а не сдвигает центроиды.")


if __name__ == "__main__":
    main()
