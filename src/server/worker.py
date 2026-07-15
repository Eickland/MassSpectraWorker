import grpc
from concurrent import futures
import asyncio
from functools import lru_cache
import numpy as np
import pandas as pd
import json

import HumSpectra.mass_spectra.assign.assign as msa
import HumSpectra.mass_spectra.raw_data_process.raw_data_process as mraw
import HumSpectra.mass_spectra.mass_descriptors.mass_descriptors as md

class MassSpectraWorker(mass_spectra_pb2_grpc.MassSpectraServiceServicer):
    
    @lru_cache(maxsize=50)
    def _get_cached_mzml(self, file_path, low_percentile, high_percentile):
        """Кеширование загрузки mzML"""
        return mraw.extract_mass_list_percentile(
            file_path, 
            low_percentile=low_percentile,
            high_percentile=high_percentile
        )
    
    def ProcessSpectra(self, request, context):
        """gRPC метод для обработки"""
        
        # Извлекаем параметры
        params = {
            'file_path': request.file_path,
            'low_percentile': request.low_percentile,
            'high_percentile': request.high_percentile,
            'rel_error': request.rel_error,
            'c_min': request.c_min,
            'c_max': request.c_max,
            # ... другие параметры
        }
        
        # Получаем кешированные данные
        ms_list = self._get_cached_mzml(
            params['file_path'],
            params['low_percentile'],
            params['high_percentile']
        )
        
        # Приписывание
        spectra = msa.assign_optimized(
            ms_list,
            brutto_dict={
                'C': (params['c_min'], params['c_max']),
                'H': (4, 80),
                'O': (0, 50),
                # ...
            },
            rel_error=params['rel_error']
        )
        
        # Расчет метрик
        spectra = md.calc_all_metrics(spectra)
        
        # Сериализация в JSON для передачи в Go
        result = {
            'data': spectra.to_dict(orient='records'),
            'columns': list(spectra.columns),
            'shape': spectra.shape
        }
        
        return mass_spectra_pb2.ProcessResponse(
            result_json=json.dumps(result),
            num_peaks=len(ms_list),
            num_assignments=len(spectra),
            processing_time_ms=0  # реальное время добавить
        )

def serve():
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    mass_spectra_pb2_grpc.add_MassSpectraServiceServicer_to_server(
        MassSpectraWorker(), server
    )
    server.add_insecure_port('[::]:50051')
    server.start()
    server.wait_for_termination()

if __name__ == '__main__':
    serve()