package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"

	openai "github.com/sashabaranov/go-openai"
)

// ========== Embedding API 封装 ==========

// embedTexts 对多条文本调用 Embedding API，返回向量列表。
// 模型名优先取 EMBEDDING_MODEL 环境变量，未设置时默认 text-embedding-3-small。
// 需要 OPENAI_API_KEY 环境变量。
func embedTexts(client *openai.Client, ctx context.Context, texts []string) ([][]float32, error) {
	model := os.Getenv("EMBEDDING_MODEL")
	if model == "" {
		model = string(openai.SmallEmbedding3)
	}
	resp, err := client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(model),
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("embedding 调用失败: %w", err)
	}

	vectors := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		vectors[i] = d.Embedding
	}
	return vectors, nil
}

// embedOne 对单条文本调用 Embedding API。
func embedOne(client *openai.Client, ctx context.Context, text string) ([]float32, error) {
	vecs, err := embedTexts(client, ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// ========== 余弦相似度 ==========

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ========== 内存向量存储 ==========

// SearchResult 单条检索结果。
type SearchResult struct {
	ID      string
	Content string
	Score   float64 // 余弦相似度，越接近 1 越相关
}

// VectorStore 内存向量索引。
// 生产环境中这部分应该用 Qdrant、Milvus 等向量数据库，
// 这里用 map 实现是为了零外部依赖，让读者能直接跑起来。
type VectorStore struct {
	vectors  map[string][]float32 // id -> embedding 向量
	payloads map[string]string    // id -> 原始文本
}

func NewVectorStore() *VectorStore {
	return &VectorStore{
		vectors:  make(map[string][]float32),
		payloads: make(map[string]string),
	}
}

// Insert 存入一条文本及其向量。
func (vs *VectorStore) Insert(id string, vector []float32, text string) {
	vs.vectors[id] = vector
	vs.payloads[id] = text
}

// Search 余弦相似度搜索，返回 top-k 结果。
func (vs *VectorStore) Search(queryVector []float32, limit int) []SearchResult {
	var results []SearchResult
	for id, vec := range vs.vectors {
		score := cosineSimilarity(queryVector, vec)
		results = append(results, SearchResult{
			ID:      id,
			Content: vs.payloads[id],
			Score:   score,
		})
	}

	// 按相似度降序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// Size 返回已索引的文档数。
func (vs *VectorStore) Size() int {
	return len(vs.vectors)
}

// ========== 文档切片 ==========

// chunkText 按固定长度切分文本，重叠 50 字符避免切在关键信息中间。
// 这是一个简单的教学实现。生产环境中通常按段落或语义边界切分。
func chunkText(text string, chunkSize int) []string {
	runes := []rune(text)
	if len(runes) <= chunkSize {
		return []string{text}
	}

	overlap := 50
	if overlap > chunkSize/2 {
		overlap = chunkSize / 4
	}

	var chunks []string
	step := chunkSize - overlap
	for i := 0; i < len(runes); i += step {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}

// ========== 知识库索引 ==========

// indexDocuments 对一组文本建索引。
func indexDocuments(vs *VectorStore, embedClient *openai.Client, ctx context.Context, docs map[string]string) error {
	for title, content := range docs {
		// 切片
		chunks := chunkText(content, 500)

		for i, chunk := range chunks {
			id := fmt.Sprintf("%s#%d", title, i)
			vec, err := embedOne(embedClient, ctx, chunk)
			if err != nil {
				return fmt.Errorf("嵌入失败 [%s]: %w", id, err)
			}
			vs.Insert(id, vec, chunk)
		}
	}
	return nil
}
