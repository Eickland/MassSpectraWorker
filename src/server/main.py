# grpc_server.py
import grpc
from concurrent import futures
import time
from collections import Counter
import src.protobuf.plot_pb2 as pb2
import src.protobuf.plot_pb2_grpc as pb2_grpc

class TextServiceServicer(pb2_grpc.TextServiceServicer):
    """Реализация методов сервиса"""
    
    def ProcessText(self, request, context):
        """Обычный RPC - принимает запрос, возвращает ответ"""
        print(f"Получен текст: {request.text}")
        
        # Обработка текста
        if request.uppercase:
            processed = request.text.upper()
        else:
            processed = request.text
        
        # Создаем ответ
        response = pb2.TextResponse(
            original_text=request.text,
            processed_text=processed,
            length=len(request.text),
            language=request.language
        )
        
        # Добавляем метаданные
        response.metadata["processed_by"] = "grpc-python"
        response.metadata["timestamp"] = str(time.time())
        
        # Можно установить custom заголовки (trailers)
        context.set_trailer((
            ('custom-header', 'value'),
        ))
        
        return response
    
    def StreamProcessText(self, request, context):
        """Server Streaming - отправляет данные по частям"""
        text = request.text
        chunk_size = 10  # по 10 символов
        
        for i in range(0, len(text), chunk_size):
            chunk = text[i:i+chunk_size]
            is_last = (i + chunk_size >= len(text))
            
            yield pb2.TextChunk(
                content=chunk,
                chunk_number=i // chunk_size + 1,
                is_last=is_last
            )
            
            time.sleep(0.5)  # имитация задержки
    
    def AnalyzeTextStream(self, request_iterator, context):
        """Client Streaming - принимает много сообщений, возвращает один ответ"""
        full_text = []
        word_counter = Counter()
        
        for chunk in request_iterator:
            full_text.append(chunk.content)
            words = chunk.content.split()
            word_counter.update(words)
        
        combined_text = " ".join(full_text)
        
        return pb2.TextAnalysis(
            word_count=len(combined_text.split()),
            char_count=len(combined_text),
            sentence_count=combined_text.count('.') + combined_text.count('!') + combined_text.count('?'),
            word_frequency=dict(word_counter)
        )
    
    def ChatStream(self, request_iterator, context):
        """Bidirectional Streaming - общение в обе стороны"""
        for message in request_iterator:
            print(f"Пользователь {message.user_id}: {message.message}")
            
            # Эхо-ответ с префиксом
            response = pb2.ChatMessage(
                user_id="server",
                message=f"Echo: {message.message}",
                timestamp=int(time.time())
            )
            
            yield response

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
    
    # Регистрируем наш сервис
    pb2_grpc.add_TextServiceServicer_to_server(
        TextServiceServicer(), server
    )
    
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