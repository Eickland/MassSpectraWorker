# grpc_server.py
import grpc
from concurrent import futures

from src.protobuf import plot_pb2 as pb2
from src.protobuf import plot_pb2_grpc as pb2_grpc
from grpc_reflection.v1alpha import reflection

import servicer



def serve():
    """Запуск gRPC сервера"""
    # Создаем сервер
    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=10),
        options=[
            ('grpc.max_send_message_length', 50 * 1024 * 1024),  # 50 MB
            ('grpc.max_receive_message_length', 50 * 1024 * 1024),
        ]
    )
 

    pb2_grpc.add_MassListServiceServicer_to_server(
        servicer.MassListServiceServicer(), server
    ) 
    
    # Включаем рефлексию
    SERVICE_NAMES = (
        pb2.DESCRIPTOR.services_by_name['MassListService'].full_name,
        reflection.SERVICE_NAME,
    )
    reflection.enable_server_reflection(SERVICE_NAMES, server)    
    # Добавляем порт (без шифрования)
    server.add_insecure_port('[::]:50051')
    
    # Запускаем сервер
    server.start()
    print("gRPC сервер запущен на порту 50051")
    
    try:
        server.wait_for_termination()
    except KeyboardInterrupt:
        print("Остановка сервера...")
        server.stop(0)

if __name__ == '__main__':
    serve()