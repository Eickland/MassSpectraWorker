from src.protobuf import plot_pb2 as pb2
from src.protobuf import plot_pb2_grpc as pb2_grpc
import grpc

import matplotlib.pyplot as plt
from datetime import datetime
import io
import time
from collections import Counter

class MassListServiceServicer(pb2_grpc.MassListServiceServicer):
    
    def ProcessMassList(self, request, context):
        
        
        
        
        return super().ProcessMassList(request, context)

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
            
            yield pb2.TextChunk( # type: ignore
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
        
        return pb2.TextAnalysis( # type: ignore
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
            response = pb2.ChatMessage( # type: ignore
                user_id="server",
                message=f"Echo: {message.message}",
                timestamp=int(time.time())
            )
            
            yield response

class PlotServiceServicer(pb2_grpc.PlotServiceServicer):
    
    def _fig_to_bytes(self, fig, format='png', dpi=100):
        """Конвертирует фигуру matplotlib в байты"""
        buf = io.BytesIO()
        fig.savefig(buf, format=format, dpi=dpi, bbox_inches='tight')
        buf.seek(0)
        return buf.getvalue()
    
    def _create_plot(self, request):
        """Создает график matplotlib на основе запроса"""
        fig, ax = plt.subplots(figsize=(request.width or 8, request.height or 6))
        
        x = list(request.x_values)
        y = list(request.y_values)
        
        # Типы графиков
        if request.plot_type == 'line':
            ax.plot(x, y, color=request.color or 'blue', linewidth=2)
        elif request.plot_type == 'scatter':
            ax.scatter(x, y, color=request.color or 'blue', s=50)
        elif request.plot_type == 'bar':
            ax.bar(x, y, color=request.color or 'blue')
        elif request.plot_type == 'hist':
            ax.hist(y, bins=20, color=request.color or 'blue', alpha=0.7)
        else:
            ax.plot(x, y, color=request.color or 'blue')
        
        # Настройки
        if request.title:
            ax.set_title(request.title)
        if request.x_label:
            ax.set_xlabel(request.x_label)
        if request.y_label:
            ax.set_ylabel(request.y_label)
        
        ax.grid(request.grid)
        
        return fig
    
    def GeneratePlot(self, request, context):
        """Создает и возвращает график"""
        try:
            # Создаем график
            fig = self._create_plot(request)
            
            # Конвертируем в байты
            format_map = {
                'png': 'png',
                'svg': 'svg',
                'pdf': 'pdf',
                'jpeg': 'jpeg',
                'jpg': 'jpeg'
            }
            fmt = format_map.get(request.format, 'png')
            
            image_bytes = self._fig_to_bytes(
                fig, 
                format=fmt, 
                dpi=request.dpi or 100
            )
            
            # Закрываем фигуру для освобождения памяти
            plt.close(fig)
            
            # Создаем ответ
            mime_types = {
                'png': 'image/png',
                'svg': 'image/svg+xml',
                'pdf': 'application/pdf',
                'jpeg': 'image/jpeg'
            }
            
            return pb2.PlotResponse(
                image_data=image_bytes,
                format=fmt,
                size_bytes=len(image_bytes),
                mime_type=mime_types.get(fmt, 'image/png'),
                generated_at=int(datetime.now().timestamp()),
                info=pb2.PlotInfo(
                    title=request.title or '',
                    width=request.width or 8,
                    height=request.height or 6,
                    dpi=request.dpi or 100
                )
            )
            
        except Exception as e:
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(f"Plot generation failed: {str(e)}")
            return pb2.PlotResponse()
    
    def StreamPlot(self, request, context):
        """Стриминг для больших графиков (чанки по 64KB)"""
        try:
            fig = self._create_plot(request)
            
            # Конвертируем в байты
            fmt = request.format or 'png'
            image_bytes = self._fig_to_bytes(fig, format=fmt, dpi=request.dpi or 100)
            plt.close(fig)
            
            # Отправляем чанками
            chunk_size = 64 * 1024  # 64KB
            total_size = len(image_bytes)
            chunk_number = 0
            
            for i in range(0, total_size, chunk_size):
                chunk = image_bytes[i:i+chunk_size]
                is_last = (i + chunk_size >= total_size)
                
                yield pb2.PlotChunk(
                    chunk_data=chunk,
                    chunk_number=chunk_number,
                    is_last=is_last,
                    total_size=total_size
                )
                chunk_number += 1
                
        except Exception as e:
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(f"Stream plot failed: {str(e)}")
    
    def GenerateMultiplePlots(self, request, context):
        """Генерирует несколько графиков"""
        for plot_req in request.plots:
            plot_req.width = request.plots[0].width or 8
            plot_req.height = request.plots[0].height or 6
            
            response = self.GeneratePlot(plot_req, context)
            yield response