# pip install pyteomics lxml
import numpy as np
from pyteomics import mzml

path = "/home/mnbv2/MassSpectraWorker/work/9a2ca153-d60c-4198-b79b-031549be2621/1LST4-Me-1.mzML"
with mzml.read(path) as reader:
    for i, s in enumerate(reader):
        mz, I = s["m/z array"], s["intensity array"]
        kind = "centroid" if "centroid spectrum" in s else "profile" if "profile spectrum" in s else "?"
        print(f"скан {i}: {kind}, пиков {len(mz)}, m/z {mz.min():.2f}–{mz.max():.2f}")
        print("  квантили интенсивности:", np.quantile(I, [0, .1, .5, .9, .99, 1]).round(0))
        w = (mz > 421.09) & (mz < 421.10)
        print("  m/z 421.09–421.10:", list(zip(mz[w].round(5), I[w].round(0))))