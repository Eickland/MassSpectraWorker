from src.protobuf import plot_pb2 as pb2
from src.protobuf import plot_pb2_grpc as pb2_grpc
import grpc

import pandas as pd
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from datetime import datetime
import os

import HumSpectra.mass_spectra.raw_data_process.raw_data_process as raw_data_process

from src.core import assign_module
from src.core import plot_service

import logging
import traceback

logging.basicConfig(level=logging.DEBUG)

def df_to_proto(df: pd.DataFrame) -> pb2.MassSpectrumTable:
    cols = []
    for name, s in df.items():
        c = pb2.Column(name=name) # type: ignore
        if pd.api.types.is_float_dtype(s):
            c.dtype = "float64"
            # NaN → null_mask, а в double_data кладём 0.0
            mask = s.isna().to_numpy().nonzero()[0]
            c.double_data.extend(s.fillna(0.0).astype("float64").tolist())
            if mask.size:
                c.null_mask.extend(mask.tolist())
        elif pd.api.types.is_integer_dtype(s):
            c.dtype = "int64"
            mask = s.isna().to_numpy().nonzero()[0]
            c.int_data.extend(s.fillna(0).astype("int64").tolist())
            if mask.size:
                c.null_mask.extend(mask.tolist())
        elif pd.api.types.is_bool_dtype(s):
            c.dtype = "bool"
            c.bool_data.extend(s.fillna(False).astype(bool).tolist())
        else:
            c.dtype = "string"
            c.str_data.extend(s.fillna("").astype(str).tolist())
            mask = s.isna().to_numpy().nonzero()[0]
            if mask.size:
                c.null_mask.extend(mask.tolist())
        cols.append(c)
    return pb2.MassSpectrumTable(columns=cols, n_rows=len(df), n_cols=df.shape[1])


class MassListServiceServicer(pb2_grpc.MassListServiceServicer):
    
    def ProcessMassList(self, request, context):
    
        try:
            logging.info(f"Processing: {request.spectra_path}")
            
            if not os.path.exists(request.spectra_path):
                error_msg = f"File not found: {request.spectra_path}"
                logging.error(error_msg)
                context.set_code(grpc.StatusCode.NOT_FOUND)
                context.set_details(error_msg)
                return pb2.MassListResponse()           
            try:
                params = assign_module.AssignParams.from_request(request)
            except ValueError as e:
                context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
                context.set_details(str(e))
                return pb2.MassListResponse()

            # 0 = не задано → значения по умолчанию функции
            percentile_kwargs = {}
            if request.low_percentile > 0:
                percentile_kwargs['low_percentile'] = request.low_percentile
            if request.high_percentile > 0:
                percentile_kwargs['high_percentile'] = request.high_percentile
            ms_list = raw_data_process.extract_mass_list_percentile(request.spectra_path, **percentile_kwargs)
            
            if ms_list is None or len(ms_list) == 0:
                error_msg = f"No data extracted from {request.spectra_path} (empty mass list), params: {request.low_percentile}, {request.high_percentile}"
                logging.error(error_msg)
                context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
                context.set_details(error_msg)
                return pb2.MassListResponse()
            
            logging.info(f"Extracted {len(ms_list)} mass peaks")
                        
            ms_spectra = assign_module.process(ms_list, request.spectra_name, params)

            if ms_spectra is None or len(ms_spectra) == 0:
                error_msg = (f"No formulas assigned for {request.spectra_name} "
                             f"(protocol={params.protocol}, rel_error={params.rel_error}, "
                             f"brutto={params.brutto_dict})")
                logging.error(error_msg)
                context.set_code(grpc.StatusCode.FAILED_PRECONDITION)
                context.set_details(error_msg)
                return pb2.MassListResponse()

            fig = plot_service.plot_preview(
                ms_spectra=ms_spectra,
                dpi=request.dpi or 100,
                width=request.width or 10,
                height=request.height or 6,
            )
            fmt = plot_service.format_map.get((request.format or 'png').lower(), 'png')
            image_bytes = plot_service._fig_to_bytes(fig, format=fmt, dpi=request.dpi or 100)
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
                spectrum=df_to_proto(ms_spectra.reset_index(drop=True))
            )
            
        except Exception as e:
            logging.error(f"Error processing {request.spectra_path}: {str(e)}")
            logging.error(traceback.format_exc())
            
            context.set_code(grpc.StatusCode.UNKNOWN)
            context.set_details(f"{str(e)}\n{traceback.format_exc()}")
            return pb2.MassListResponse()