import HumSpectra.mass_spectra.assign.assign as assign
import HumSpectra.mass_spectra.mass_descriptors.mass_descriptors as mass_descriptors
import HumSpectra.mass_spectra.calibration.calibration as calibration
import HumSpectra.mass_spectra.calc_process.calc_process as calc_process

def process_non_tmds(ms_list, name):
    """Обработка non-TMDS данных"""
    print(f"Обработка non-TMDS для {name}")
    
    ms_list.attrs['name'] = name
    ms_list = calibration.recallibrate_optimize(ms_list, draw=False)
    
    spectra = assign.assign_optimized(
        ms_list, 
        brutto_dict={'C': (4, 50), 'H': (4, 80), 'O': (0, 50), 
                     'N': (0, 3), 'C_13': (0, 1), 'S': (0, 3)},
        rel_error=0.5, 
        sulfur_precision_factor=10,
        nitrogen_precision_factor=4,
        charge_max=3
    )
    
    # Фильтрация
    spectra_non_tmds = spectra.loc[(spectra['C'] != 0) & 
                                    (spectra['H'] != 0) & 
                                    (spectra['O'] != 0)]
    
    return spectra_non_tmds