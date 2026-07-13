import HumSpectra.mass_spectra.raw_data_process.raw_data_process as mraw
import HumSpectra.utilits as ut
from typing import List, Dict, Optional, Tuple, Union, Any

class MzmlReader:
    
    def __init__(self, path:str, param):
        
        name = ut.extract_name_from_path(path)
        name = ut.delete_series_number(name)
        
        ms_list = mraw.extract_mass_list_percentile(path, low_percentile=low_percentile, high_percentile=high_percentile)        