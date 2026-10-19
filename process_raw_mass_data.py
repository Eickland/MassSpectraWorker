from pyopenms import * # type: ignore
from pathlib import Path
import pandas as pd
import HumSpectra.mass_spectra.assign.assign as msa
import HumSpectra.mass_spectra.visual.visual as msv
import HumSpectra.mass_spectra.mass_descriptors.mass_descriptors as md
import HumSpectra.mass_spectra.calc_process.calc_process as mcalc
import HumSpectra.mass_spectra.tmds.tmds as mtmds
import HumSpectra.mass_spectra.calibration.calibration as mc
import HumSpectra.mass_spectra.utilits.utilits as mut
import HumSpectra.mass_spectra.raw_data_process.raw_data_process as mraw
import HumSpectra.utilits as ut
import matplotlib.pyplot as plt
import numpy as np
from scipy.ndimage import gaussian_filter1d
import os
import xml.etree.ElementTree as ET
import gzip
import subprocess
from pathlib import Path
   
root_path = r"C:\Users\Kirill\Desktop\Lab\article_diplom\mzml_data"
source_dir = "C:\\Users\\Kirill\\Desktop\\Lab\\article_diplom\\raw_data"
tmds_path = Path(r'C:\Users\Kirill\Desktop\Lab\article_diplom\process_data\tmds')
non_tmds_path = Path(r'C:\Users\Kirill\Desktop\Lab\article_diplom\process_data\non_tmds')
alt_program_path = "C:\\Users\\Kirill\\AppData\\Local\\Apps\\ProteoWizard 3.0.25287.154e9d0 64-bit"
# Флаги для включения/отключения обработки
PROCESS_NON_TMDS = True   # Включить обработку non-TMDS
PROCESS_TMDS = False       # Включить обработку TMDS
RAW_MODE = False          # Режим raw (без полной обработки)
FULL_CYCLE_MODE = True
SINGLE_SPECTRA_MODE = False
CONVERT_TO_MZML = False
CONVERT_TO_TXT =False
PROCESS_SPECTRUM = False

def process_non_tmds(ms_list, name, save_path):
    """Обработка non-TMDS данных"""
    print(f"Обработка non-TMDS для {name}")
    
    ms_list.attrs['name'] = name
    ms_list = mc.recallibrate_optimize(ms_list, draw=False)
    
    spectra = msa.assign_optimized(
        ms_list, 
        brutto_dict={'C': (4, 50), 'H': (4, 80), 'O': (0, 50), 
                     'N': (0, 3), 'C_13': (0, 1), 'S': (0, 3)},
        rel_error=1, 
        sulfur_precision_factor=10,
        nitrogen_precision_factor=4,
        charge_max=3
    )
    
    # Фильтрация
    spectra_non_tmds = spectra.loc[(spectra['C'] != 0) & 
                                    (spectra['H'] != 0) & 
                                    (spectra['O'] != 0)]
    
    # Нормализация и расчет метрик
    spectra_non_tmds = mcalc.normalize(md.calc_all_metrics(spectra_non_tmds))
    spectra_non_tmds = spectra_non_tmds.loc[
        spectra_non_tmds.groupby('brutto')['rel_error'].apply(
            lambda x: (x.abs() == x.abs().min()).idxmax()
        )
    ].reset_index(drop=True)
    spectra_non_tmds = md.mol_class(spectra_non_tmds, how="perminova")
    
    # Создание директорий и сохранение
    save_results(spectra_non_tmds, name, save_path, 'non_tmds')
    return spectra_non_tmds

def process_tmds(ms_list, name, save_path):
    """Обработка TMDS данных"""
    print(f"Обработка TMDS для {name}")
    
    ms_list.attrs['name'] = name
    ms_list = mc.recallibrate_optimize(ms_list, draw=False)
    
    spectra = msa.assign_optimized(
        ms_list,
        brutto_dict={'C': (4, 50), 'H': (4, 80), 'O': (0, 50),
                     'N': (0, 3), 'C_13': (0, 1), 'S': (0, 3)},
        rel_error=0.5,
        sulfur_precision_factor=10,
        nitrogen_precision_factor=4
    )
    
    # Расчет TMDS спектров
    spectra = mut.calc_mass(spectra)
    tmds_spectra = mtmds.calc_by_brutto(spectra)
    tmds_spectra = msa.assign_optimized(
        tmds_spectra,
        brutto_dict={'C': (-1, 20), 'H': (-4, 40), 'O': (-1, 20), 'N': (0, 1)}
    )
    
    tmds_spectra = mut.calc_mass(tmds_spectra, debug=False)
    spectra = mtmds.assign_by_tmds_optimize(spectra, tmds_spectra, rel_error=0.5, max_num=100)
    
    # Фильтрация
    spectra = spectra.loc[(spectra['C'] != 0) & 
                          (spectra['H'] != 0) & 
                          (spectra['O'] != 0)]
    
    # Нормализация и расчет метрик
    spectra = mcalc.normalize(md.calc_all_metrics(spectra))
    spectra = spectra.loc[
        spectra.groupby('brutto')['rel_error'].apply(
            lambda x: (x.abs() == x.abs().min()).idxmax()
        )
    ].reset_index(drop=True)
    spectra = md.mol_class(spectra, how="perminova")
    
    # Создание директорий и сохранение
    save_results(spectra, name, save_path, 'tmds')
    return spectra

def process_raw_mode(ms_list, name, save_path):
    """Обработка в raw режиме (только интерактивный график)"""
    print(f"Raw режим для {name}")
    
    fig = msv.interactive_spectrum_plotly(ms_list, normalize=False, max_points=30000)
    html_path = Path.joinpath(save_path, 'raw', f'{name}__spectrum.html')
    fig.write_html(html_path)

def save_results(spectra, name, save_path, prefix):
    """Сохранение результатов обработки"""
    paths = {
        'mzlist': Path.joinpath(save_path, 'mzlist'),
        'statistic': Path.joinpath(save_path, 'statistic'),
        'vk': Path.joinpath(save_path, 'vk'),
        'mass_scatter': Path.joinpath(save_path, 'mass_scatter'),
        'density': Path.joinpath(save_path, 'density'),
        'mir': Path.joinpath(save_path, 'mir'),
        'bar': Path.joinpath(save_path, 'bar'),
        'pie': Path.joinpath(save_path, 'pie'),
        'comp': Path.joinpath(save_path, 'comp'),
        'spectrum': Path.joinpath(save_path, 'spectrum')
    }
    
    # Создание директорий
    for path in paths.values():
        path.mkdir(parents=True, exist_ok=True)
    
    # Сохранение файлов
    spectra.dropna().to_csv(paths['mzlist'] / f'{name}__mzlist.csv', sep=',', index=False)
    spectra.dropna().describe().to_excel(paths['statistic'] / f'{name}__statistic.xlsx')
    
    # Сохранение графиков
    plots_config = [
        (msv.vk, {'sizes': (8, 50)}, paths['vk'], f'{name}__vk.png'),
        (msv.vk, {'sizes': (8, 50), 'plot_type': 'mass_scatter'}, paths['mass_scatter'], f'{name}__mass_scatter.png'),
        (msv.vk, {'sizes': (8, 50), 'plot_type': 'density'}, paths['density'], f'{name}__density.png'),
        (msv.plot_mass_intensity_relationship, {}, paths['mir'], f'{name}__mir.png'),
        (msv.plot_mol_class_distribution, {'mod': 'bar'}, paths['bar'], f'{name}__bar.png'),
        (msv.plot_mol_class_distribution, {'mod': 'pie'}, paths['pie'], f'{name}__pie.png'),
        (msv.elemental_composition, {}, paths['comp'], f'{name}__comp.png'),
        (msv.spectrum, {'xlim': (200, 800)}, paths['spectrum'], f'{name}__spectrum.png')
    ]
    
    for plot_func, kwargs, save_dir, filename in plots_config:
        try:
            plot_func(spectra, **kwargs)
            plt.savefig(save_dir / filename)
            plt.close()
        except Exception as e:
            print(f"Ошибка при сохранении графика {filename}: {e}")
            plt.close()

def process_single_file(windows_path, name, document_save_path_non_tmds, document_save_path_tmds, **args):
    """Обработка одного файла"""
    path = str(windows_path)
    print(f"\nОбработка файла: {name}")
    
    low_percentile = args.get('low_percentile', 99.3)
    high_percentile = args.get('high_percentile', 99.99)
    
    # Извлечение масс-списка в зависимости от режима
    if RAW_MODE:
        ms_list = mraw.extract_mass_list_percentile(path, low_percentile=low_percentile)
        process_raw_mode(ms_list, name, document_save_path_non_tmds)
        ms_list.to_csv(Path.joinpath(document_save_path_non_tmds, 'raw', f'{name}__raw.csv'))
        return
    
    # Полная обработка
    ms_list = mraw.extract_mass_list_percentile(path, low_percentile=low_percentile, high_percentile=high_percentile)
    print(f'Число пиков: {len(ms_list)}')
    
    # Обработка non-TMDS
    if PROCESS_NON_TMDS:
        process_non_tmds(ms_list.copy(), name, document_save_path_non_tmds)
    
    # Обработка TMDS
    if PROCESS_TMDS:
        process_tmds(ms_list.copy(), name, document_save_path_tmds)

def mzml_to_txt_batch_pyopenms(source_dir, output_base=None, ms_level=1, rt_range=None):
    """
    Преобразует все mzML файлы в текстовые файлы с колонками m/z и intensity
    используя pyOpenMS
    
    Args:
        source_dir (str): Путь к папке с mzML файлами
        output_base (str): Путь для сохранения TXT файлов (если None, сохраняет рядом с mzML)
        ms_level (int): Уровень MS спектров (1 - MS1, 2 - MS2 и т.д.)
        rt_range (tuple): Диапазон времени удерживания (min, max) или None
    """
    
    # Если output_base не указан, используем source_dir
    if output_base is None:
        output_base = source_dir
    
    # Создаем папку для результатов, если её нет
    os.makedirs(output_base, exist_ok=True)
    
    # Находим все mzML файлы
    mzml_files = []
    for root, dirs, files in os.walk(source_dir):
        for file in files:
            if file.lower().endswith('.mzml') or file.lower().endswith('.mzml.gz'):
                mzml_files.append(os.path.join(root, file))
    
    print(f"🔍 Найдено {len(mzml_files)} mzML файлов")
    
    successful = 0
    failed = 0
    
    for mzml_path in mzml_files:
        try:
            # Определяем путь для выходного TXT файла
            relative_path = os.path.relpath(mzml_path, source_dir)
            txt_filename = os.path.splitext(os.path.basename(mzml_path))[0] + '.txt'
            
            if output_base == source_dir:
                # Сохраняем в той же папке, где находится mzML
                txt_path = os.path.join(os.path.dirname(mzml_path), txt_filename)
            else:
                # Сохраняем в output_base с сохранением структуры
                txt_dir = os.path.join(output_base, os.path.dirname(relative_path))
                os.makedirs(txt_dir, exist_ok=True)
                txt_path = os.path.join(txt_dir, txt_filename)
            
            print(f"🔄 Конвертируем: {os.path.basename(mzml_path)} -> {txt_filename}")
            
            # Конвертируем mzML в TXT
            convert_single_mzml_to_txt_pyopenms(mzml_path, txt_path, ms_level, rt_range)
            successful += 1
            
        except Exception as e:
            print(f"❌ Ошибка при конвертации {mzml_path}: {e}")
            failed += 1
    
    print(f"✅ Конвертация завершена! Успешно: {successful}, Ошибок: {failed}")

def convert_single_mzml_to_txt_pyopenms(mzml_file, txt_file, ms_level=1, rt_range=None):
    """
    Конвертирует один mzML файл в TXT с колонками m/z и intensity
    
    Args:
        mzml_file (str): Путь к mzML файлу
        txt_file (str): Путь для сохранения TXT файла
        ms_level (int): Уровень MS спектров
        rt_range (tuple): Диапазон времени удерживания (min, max) или None
    """
    
    # Загружаем mzML файл
    exp = MSExperiment()
    MzMLFile().load(mzml_file, exp)
    
    # Собираем все пики
    all_mz = []
    all_intensities = []
    spectrum_info = []  # Для разделения спектров
    
    for spectrum_index, spectrum in enumerate(exp):
        # Проверяем уровень MS
        if spectrum.getMSLevel() != ms_level:
            continue
        
        # Проверяем время удерживания
        if rt_range is not None:
            rt = spectrum.getRT()
            if not (rt_range[0] <= rt <= rt_range[1]):
                continue
        
        # Получаем пики
        mz, intensities = spectrum.get_peaks()
        
        if len(mz) > 0:
            # Добавляем все пики
            all_mz.extend(mz)
            all_intensities.extend(intensities)
            
            # Запоминаем границы спектров
            if len(spectrum_info) > 0:
                spectrum_info.append(spectrum_info[-1] + len(mz))
            else:
                spectrum_info.append(len(mz))
    
    if not all_mz:
        print(f"⚠️ В файле {os.path.basename(mzml_file)} нет данных для MS уровня {ms_level}")
        # Создаем пустой файл с заголовком
        with open(txt_file, 'w') as f:
            f.write("m/z\tintensity\n")
        return
    
    # Создаем DataFrame
    df = pd.DataFrame({
        'm/z': all_mz,
        'intensity': all_intensities
    })
    
    # Сохраняем в TXT с разделением спектров
    with open(txt_file, 'w') as f:
        # Пишем заголовок
        f.write("m/z\tintensity\n")
        
        # Пишем данные по спектрам
        start_idx = 0
        for i, end_idx in enumerate(spectrum_info):
            # Записываем данные текущего спектра
            for j in range(start_idx, end_idx):
                f.write(f"{df.iloc[j]['m/z']:.6f}\t{df.iloc[j]['intensity']:.6f}\n")
            
            # Добавляем пустую строку между спектрами
            if i < len(spectrum_info) - 1:
                f.write("\n")
            
            start_idx = end_idx
    
    print(f"  📊 Записано {len(df)} пиков из {len(spectrum_info)} спектров")

if CONVERT_TO_MZML:

    mut.convert_mass_spectra_batch(source_dir=source_dir,output_base=root_path,
                                   program_location=alt_program_path)

if CONVERT_TO_TXT:

    mzml_to_txt_batch_pyopenms(
        source_dir=r"D:\lab\article_humification\mass_spectra\raw_data_sludge_lignin",
        output_base=r"D:\lab\article_humification\mass_spectra\raw_data_sludge_lignin_txt"
    )    

if FULL_CYCLE_MODE:
    
    folder_path = str(root_path)
    this_document_save_path_tmds = tmds_path
    this_document_save_path_non_tmds = non_tmds_path
    
    for windows_path in Path(root_path).rglob('*.mzML'):
        name = ut.extract_name_from_path(str(windows_path))
        name = ut.delete_series_number(name)
        
        
        process_single_file(windows_path, name,document_save_path_non_tmds=this_document_save_path_non_tmds,
                            document_save_path_tmds=this_document_save_path_tmds,low_percentile=99.7,high_percentile=99.992)

    print("\nОбработка завершена!")
      
    


       