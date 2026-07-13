// grpc_client.go
package grpc_client

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	// Сгенерированный код из .proto
	pb "Mass_spectra_worker/src/protobuf/plot" // измените на ваш путь
)

type TextServiceClient struct {
	client pb.TextServiceClient
	conn   *grpc.ClientConn
}

// NewTextServiceClient создает нового клиента
func NewTextServiceClient(addr string) (*TextServiceClient, error) {
	// Создаем соединение с сервером
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(50*1024*1024), // 50 MB
			grpc.MaxCallSendMsgSize(50*1024*1024),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	client := pb.NewTextServiceClient(conn)

	return &TextServiceClient{
		client: client,
		conn:   conn,
	}, nil
}

// Close закрывает соединение
func (c *TextServiceClient) Close() error {
	return c.conn.Close()
}

// 1. Обычный RPC (Unary)
func (c *TextServiceClient) ProcessText(ctx context.Context, text, language string, uppercase bool) (*pb.TextResponse, error) {
	// Создаем запрос
	req := &pb.TextRequest{
		Text:      text,
		Language:  language,
		Uppercase: uppercase,
		Tags:      []string{"go-client", "example"},
	}

	// Добавляем метаданные (аналог HTTP заголовков)
	md := metadata.Pairs(
		"client-id", "go-client",
		"client-version", "1.0.0",
	)
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Устанавливаем таймаут
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Вызываем метод
	resp, err := c.client.ProcessText(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ProcessText failed: %w", err)
	}

	return resp, nil
}

// 2. Server Streaming - получение данных по частям
func (c *TextServiceClient) StreamProcessText(ctx context.Context, text string) error {
	req := &pb.TextRequest{
		Text: text,
	}

	// Вызываем стриминг метод
	stream, err := c.client.StreamProcessText(ctx, req)
	if err != nil {
		return fmt.Errorf("StreamProcessText failed: %w", err)
	}

	fmt.Println("=== Server Streaming ===")
	for {
		// Получаем следующий чанк
		chunk, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" {
				break // Стрим завершен
			}
			return fmt.Errorf("error receiving chunk: %w", err)
		}

		fmt.Printf("Чанк %d: '%s' (последний: %v)\n",
			chunk.ChunkNumber, chunk.Content, chunk.IsLast)
	}

	return nil
}

// 3. Client Streaming - отправка нескольких сообщений
func (c *TextServiceClient) AnalyzeTextStream(ctx context.Context, chunks []string) (*pb.TextAnalysis, error) {
	// Создаем стрим
	stream, err := c.client.AnalyzeTextStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("AnalyzeTextStream failed: %w", err)
	}

	// Отправляем все чанки
	for i, chunk := range chunks {
		req := &pb.TextChunk{
			Content:     chunk,
			ChunkNumber: int32(i + 1),
			IsLast:      i == len(chunks)-1,
		}

		if err := stream.Send(req); err != nil {
			return nil, fmt.Errorf("failed to send chunk: %w", err)
		}

		fmt.Printf("Отправлен чанк %d: '%s'\n", i+1, chunk)
		time.Sleep(100 * time.Millisecond)
	}

	// Закрываем отправку и получаем ответ
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, fmt.Errorf("failed to receive analysis: %w", err)
	}

	return resp, nil
}

// 4. Bidirectional Streaming - интерактивный чат
func (c *TextServiceClient) ChatStream(ctx context.Context, messages []string) error {
	// Создаем стрим
	stream, err := c.client.ChatStream(ctx)
	if err != nil {
		return fmt.Errorf("ChatStream failed: %w", err)
	}

	// Канал для сигнала завершения
	done := make(chan bool)

	// Горутина для получения сообщений от сервера
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				if err.Error() != "EOF" {
					log.Printf("Error receiving: %v", err)
				}
				done <- true
				return
			}

			timestamp := time.Unix(resp.Timestamp, 0)
			fmt.Printf("Сервер [%s]: %s\n",
				timestamp.Format("15:04:05"), resp.Message)
		}
	}()

	// Отправляем сообщения
	for _, msg := range messages {
		req := &pb.ChatMessage{
			UserId:    "go-client",
			Message:   msg,
			Timestamp: time.Now().Unix(),
		}

		if err := stream.Send(req); err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}

		fmt.Printf("Клиент: %s\n", msg)
		time.Sleep(500 * time.Millisecond)
	}

	// Закрываем отправку
	stream.CloseSend()

	// Ждем завершения получения
	<-done
	return nil
}

// 5. Дополнительный метод с использованием контекста и каналов
func (c *TextServiceClient) ProcessTextWithRetry(ctx context.Context, text, language string, uppercase bool, maxRetries int) (*pb.TextResponse, error) {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		resp, err := c.ProcessText(ctx, text, language, uppercase)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		log.Printf("Попытка %d/%d завершилась ошибкой: %v", i+1, maxRetries, err)

		// Экспоненциальная задержка
		backoff := time.Duration(1<<uint(i)) * time.Second
		time.Sleep(backoff)
	}

	return nil, fmt.Errorf("all retries failed: %w", lastErr)
}

// 6. Парсинг метаданных из ответа
func (c *TextServiceClient) ProcessTextWithMetadata(ctx context.Context, text string) (*pb.TextResponse, metadata.MD, error) {
	req := &pb.TextRequest{
		Text: text,
	}

	// Создаем переменную для хранения метаданных
	var header, trailer metadata.MD

	// Вызываем метод с колбэками для метаданных
	resp, err := c.client.ProcessText(
		ctx,
		req,
		grpc.Header(&header),   // заголовки
		grpc.Trailer(&trailer), // трейлеры
	)
	if err != nil {
		return nil, nil, err
	}

	fmt.Printf("Заголовки: %v\n", header)
	fmt.Printf("Трейлеры: %v\n", trailer)

	return resp, header, nil
}

func main() {
	// Создаем клиента
	client, err := NewTextServiceClient("localhost:50051")
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// 1. Обычный RPC
	fmt.Println("=== 1. Обычный RPC ===")
	resp, err := client.ProcessText(ctx, "Привет, мир!", "ru", true)
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("Оригинал: %s\n", resp.OriginalText)
		fmt.Printf("Обработан: %s\n", resp.ProcessedText)
		fmt.Printf("Длина: %d\n", resp.Length)
		fmt.Printf("Язык: %s\n", resp.Language)
		fmt.Printf("Метаданные: %v\n", resp.Metadata)
	}
	fmt.Println()

	// 2. Server Streaming
	fmt.Println("=== 2. Server Streaming ===")
	err = client.StreamProcessText(ctx, "Это длинный текст, который будет отправлен по частям!")
	if err != nil {
		log.Printf("Error: %v", err)
	}
	fmt.Println()

	// 3. Client Streaming
	fmt.Println("=== 3. Client Streaming ===")
	chunks := []string{"Hello", "world", "from", "gRPC", "Go", "client"}
	analysis, err := client.AnalyzeTextStream(ctx, chunks)
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("Слов: %d\n", analysis.WordCount)
		fmt.Printf("Символов: %d\n", analysis.CharCount)
		fmt.Printf("Предложений: %d\n", analysis.SentenceCount)
		fmt.Printf("Частота слов: %v\n", analysis.WordFrequency)
	}
	fmt.Println()

	// 4. Bidirectional Streaming
	fmt.Println("=== 4. Bidirectional Streaming ===")
	messages := []string{"Привет из Go!", "Как дела?", "Пока!"}
	err = client.ChatStream(ctx, messages)
	if err != nil {
		log.Printf("Error: %v", err)
	}
	fmt.Println()

	// 5. С ретраями
	fmt.Println("=== 5. С Retry механизмом ===")
	respWithRetry, err := client.ProcessTextWithRetry(ctx, "Текст с retry", "ru", false, 3)
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("Успешно с retry: %s\n", respWithRetry.ProcessedText)
	}
	fmt.Println()

	// 6. С получением метаданных
	fmt.Println("=== 6. С метаданными ===")
	_, header, err := client.ProcessTextWithMetadata(ctx, "Текст с метаданными")
	if err != nil {
		log.Printf("Error: %v", err)
	}
}

// 7. Пример с использованием контекстного таймаута и отмены
func (c *TextServiceClient) ProcessWithTimeoutAndCancel(parentCtx context.Context, text string) {
	// Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(parentCtx, 2*time.Second)
	defer cancel() // важно вызвать для освобождения ресурсов

	// Вызов с контекстом
	resp, err := c.ProcessText(ctx, text, "ru", false)
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Результат: %s\n", resp.ProcessedText)
}

// 8. Пул соединений для множества запросов
type ClientPool struct {
	clients []*TextServiceClient
	index   int
	mu      sync.Mutex
}

func NewClientPool(addr string, size int) (*ClientPool, error) {
	clients := make([]*TextServiceClient, size)
	for i := 0; i < size; i++ {
		client, err := NewTextServiceClient(addr)
		if err != nil {
			return nil, err
		}
		clients[i] = client
	}

	return &ClientPool{
		clients: clients,
	}, nil
}

func (p *ClientPool) GetClient() *TextServiceClient {
	p.mu.Lock()
	defer p.mu.Unlock()

	client := p.clients[p.index]
	p.index = (p.index + 1) % len(p.clients)
	return client
}
