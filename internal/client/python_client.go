// pkg/grpcclient/client.go
package grpcclient

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	pb "MassSpectraWorker/src/protobuf"
)

var (
	instance *GRPCClient
	once     sync.Once
)

// GRPCClient - синглтон для gRPC клиента
type GRPCClient struct {
	conn   *grpc.ClientConn
	client pb.PlotServiceClient
	mu     sync.RWMutex
}

// GetClient - возвращает единственный экземпляр клиента
func GetClient() (*GRPCClient, error) {
	var err error
	once.Do(func() {
		instance, err = newGRPCClient()
	})
	return instance, err
}

// newGRPCClient - создает новый gRPC клиент
func newGRPCClient() (*GRPCClient, error) {
	// Настройки keepalive для постоянного соединения
	keepaliveParams := keepalive.ClientParameters{
		Time:                10 * time.Second, // ping каждые 10 секунд
		Timeout:             3 * time.Second,  // таймаут ping
		PermitWithoutStream: true,             // разрешить ping без активных стримов
	}

	// Создаем соединение с сервером
	conn, err := grpc.NewClient(
		"localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepaliveParams),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(50*1024*1024), // 50MB
			grpc.MaxCallSendMsgSize(50*1024*1024),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	client := &GRPCClient{
		conn:   conn,
		client: pb.NewPlotServiceClient(conn),
	}

	log.Println("✅ gRPC client connected to localhost:50051")
	return client, nil
}

// GetPlotServiceClient - возвращает gRPC клиент для вызовов
func (c *GRPCClient) GetPlotServiceClient() pb.PlotServiceClient {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

// Close - закрывает соединение
func (c *GRPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsHealthy - проверяет здоровье соединения
func (c *GRPCClient) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return false
	}

	// Проверяем состояние соединения
	state := c.conn.GetState()
	return state == connectivity.Ready
}

// GeneratePlot - обертка для вызова gRPC метода
func (c *GRPCClient) GeneratePlot(ctx context.Context, req *pb.PlotRequest) (*pb.PlotResponse, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	// Устанавливаем таймаут если его нет
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	return client.GeneratePlot(ctx, req)
}

// StreamPlot - обертка для стриминга
func (c *GRPCClient) StreamPlot(ctx context.Context, req *pb.PlotRequest) (pb.PlotService_StreamPlotClient, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	}

	return client.StreamPlot(ctx, req)
}
