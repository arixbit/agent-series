package main

import (
	"fmt"
)

// estimateTokens 字符数粗估 token 数。保守估计 1 token ≈ 2 字符。
// 实际结果以 API 返回的 usage 为准，这个粗估仅用于上下文预算管理。
func estimateTokens(text string) int {
	return len([]rune(text))/2 + 1
}

func main() {
	loadEnv(".env")

	// 英文
	en := "Hello, how are you? I'm writing a Go program to call the LLM API."
	fmt.Printf("英文: %s\n", en)
	fmt.Printf("估算 token: %d\n\n", estimateTokens(en))

	// 纯中文（不含拉丁字符）
	cn := "你好，最近怎么样？我正在写一个程序来调用人工智能的接口。"
	fmt.Printf("中文: %s\n", cn)
	fmt.Printf("估算 token: %d\n", estimateTokens(cn))

	// 更准的近似可用 tiktoken（cl100k_base 与 DeepSeek 分词器接近但不完全一致）：
	//   go get github.com/pkoukk/tiktoken-go@latest
	//   tke, err := tiktoken.GetEncoding("cl100k_base")
	// 注意：GetEncoding 首次运行会从 openaipublic.blob.core.windows.net
	// 下载 tokenizer 文件，大陆网络需要配置代理。
	// DeepSeek 精确 token 数以 API 返回的 usage 或官方离线 tokenizer 为准。
}
