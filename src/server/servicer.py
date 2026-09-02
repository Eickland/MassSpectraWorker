from src.protobuf import plot_pb2 as pb2
from src.protobuf import plot_pb2_grpc as pb2_grpc
import grpc

import pandas as pd

import matplotlib.pyplot as plt
from datetime import datetime
import io
import time
from collections import Counter

import HumSpectra.mass_spectra.raw_data_process.raw_data_process as raw_data_process

from src.core import assign_module
from src.core import plot_service

import logging
import traceback

logging.basicConfig(level=logging.DEBUG)


class MassListServiceServicer(pb2_grpc.MassListServiceServicer):
    
    def ProcessMassList(self, request, context):
    
        try:
            logging.info(f"Processing: {request.spectra_path}")
            
            import os
            if not os.path.exists(request.spectra_path):
                error_msg = f"File not found: {request.spectra_path}"
                logging.error(error_msg)
                context.set_code(grpc.StatusCode.NOT_FOUND)
                context.set_details(error_msg)
                return pb2.MassListResponse()
                           
            ms_list = raw_data_process.extract_mass_list_percentile(request.spectra_path, low_percentile=request.low_percentile,high_percentile=request.high_percentile)
            
            if ms_list is None or len(ms_list) == 0:
                error_msg = f"No data extracted from {request.spectra_path} (empty mass list)"
                logging.error(error_msg)
                context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
                context.set_details(error_msg)
                return pb2.MassListResponse()
            
            logging.info(f"Extracted {len(ms_list)} mass peaks")
                        
            ms_spectra = assign_module.process_non_tmds(ms_list, request.spectra_name)

            fig = plot_service.plot_preview(ms_spectra=ms_spectra,dpi=request.dpi, width=request.width,height=request.height)
            fmt = plot_service.format_map.get(request.format, 'png')
            image_bytes = plot_service._fig_to_bytes(fig, format=fmt, dpi=request.dpi)
            plt.close(fig)
            
            mime_types = {
                'png': 'image/png',
                'svg': 'image/svg+xml',
                'pdf': 'application/pdf',
                'jpeg': 'image/jpeg'
            }

            
            return pb2.MassListResponse(
                image_data=image_bytes,
                format=fmt,
                size_bytes=len(image_bytes),
                mime_type=mime_types.get(fmt, 'image/png'),
                generated_at=int(datetime.now().timestamp()),
            )
            
        except Exception as e:
            logging.error(f"Error processing {request.spectra_path}: {str(e)}")
            logging.error(traceback.format_exc())
            
            context.set_code(grpc.StatusCode.UNKNOWN)
            context.set_details(f"{str(e)}\n{traceback.format_exc()}")
            return pb2.MassListResponse()