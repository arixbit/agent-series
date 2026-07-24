package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/arixbit/agent-series/11-tools-mcp-skills/weather"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpPath = "/mcp"

func main() {
	address := flag.String("addr", "127.0.0.1:8080", "HTTP 监听地址")
	flag.Parse()

	server := newWeatherServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
	mux := http.NewServeMux()
	mux.Handle(mcpPath, handler)

	httpServer := &http.Server{
		Addr:              *address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("天气 MCP Server 已启动：http://%s%s", *address, mcpPath)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func newWeatherServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "mock-weather-server",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_weather",
		Description: "查询城市的 mock 天气",
	}, weather.Get)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_supported_cities",
		Description: "列出当前有 mock 天气数据的城市",
	}, weather.ListSupportedCities)

	return server
}
