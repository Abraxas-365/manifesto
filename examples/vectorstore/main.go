package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/ai/document"
	"github.com/Abraxas-365/manifesto/pkg/ai/embedding"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm"
	"github.com/Abraxas-365/manifesto/pkg/ai/providers/aiopenai"
	"github.com/Abraxas-365/manifesto/pkg/ai/vstore"
	"github.com/Abraxas-365/manifesto/pkg/ai/vstore/providers/vstmemory"
	"github.com/Abraxas-365/manifesto/pkg/fsx/fsxlocal"
)

func main() {
	ctx := context.Background()

	fmt.Println("🚀 In-Memory Vector Store Example")
	fmt.Println("=" + strings.Repeat("=", 70))
	fmt.Println("✅ No PostgreSQL needed!")
	fmt.Println()

	// ========================================================================
	// STEP 1: Create In-Memory Vector Store
	// ========================================================================

	fmt.Println("💾 Creating in-memory vector store...")

	// Create in-memory provider
	memoryProvider := vstmemory.NewMemoryVectorStore(
		1536,                // dimension
		vstore.MetricCosine, // similarity metric
	)

	vectorStore := vstore.NewClient(memoryProvider)

	fmt.Println("✅ In-memory vector store ready (no database required!)")

	// ========================================================================
	// STEP 2: Setup Embedder
	// ========================================================================

	fmt.Println("\n🤖 Setting up embedder...")

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY not set")
	}

	openaiProvider := aiopenai.NewOpenAIProvider(apiKey)
	embedder := document.NewEmbedder(
		openaiProvider,
		1536,
		embedding.WithModel("text-embedding-3-small"),
	)

	fmt.Println("✅ Embedder ready")

	// ========================================================================
	// STEP 3: Create Document Store
	// ========================================================================

	docStore := document.NewDocumentStore(vectorStore, embedder).
		WithNamespace("knowledge-base").
		WithBatchSize(50)

	// ========================================================================
	// STEP 4: Setup Local File System
	// ========================================================================

	fmt.Println("\n📁 Setting up local file system...")

	localFS, err := fsxlocal.NewLocalFileSystem("./data")
	if err != nil {
		log.Fatal(err)
	}

	// Create sample documents
	if err := createSampleDocuments(ctx, localFS); err != nil {
		log.Fatal(err)
	}

	// ========================================================================
	// STEP 5: Ingest Documents
	// ========================================================================

	fmt.Println("\n📥 Ingesting documents...")

	if err := ingestDocuments(ctx, localFS, docStore); err != nil {
		log.Fatal(err)
	}

	// ========================================================================
	// STEP 6: Search Documents
	// ========================================================================

	fmt.Println("\n🔍 Searching documents...")

	searchExamples(ctx, docStore)

	// ========================================================================
	// STEP 7: RAG Example
	// ========================================================================

	fmt.Println("\n🧠 RAG Example...")

	llmClient := llm.NewClient(openaiProvider)
	performRAG(ctx, docStore, llmClient, "What is machine learning and why is it important?")

	// ========================================================================
	// STEP 8: Show Statistics
	// ========================================================================

	fmt.Println("\n📊 Statistics...")

	stats, _ := docStore.GetStats(ctx)
	fmt.Printf("Total vectors: %d\n", stats.TotalVectorCount)
	fmt.Printf("Dimension: %d\n", stats.Dimension)

	if len(stats.Namespaces) > 0 {
		fmt.Println("Namespaces:")
		for _, ns := range stats.Namespaces {
			fmt.Printf("  - %s: %d vectors\n", ns.Name, ns.VectorCount)
		}
	}

	// ========================================================================
	// STEP 9: Demonstrate Advanced Features
	// ========================================================================

	fmt.Println("\n🎯 Advanced Features...")

	demonstrateFiltering(ctx, docStore)
	demonstrateNamespaces(ctx, vectorStore, embedder)

	fmt.Println("\n✅ Example completed!")
	fmt.Printf("\nℹ️  Total vectors in memory: %d\n", memoryProvider.Count())
}

// ============================================================================
// Document Creation and Ingestion
// ============================================================================

func createSampleDocuments(ctx context.Context, fs *fsxlocal.LocalFileSystem) error {
	documents := map[string]string{
		"ml_intro.txt": `Machine Learning Fundamentals

Machine learning is a subset of artificial intelligence that enables systems to learn and improve from experience without being explicitly programmed. It focuses on developing algorithms that can access data and use it to learn for themselves.

Key Types:
1. Supervised Learning - Learning from labeled data
2. Unsupervised Learning - Finding patterns in unlabeled data  
3. Reinforcement Learning - Learning through rewards and penalties

Applications:
- Image and speech recognition
- Medical diagnosis
- Stock market predictions
- Recommendation systems
- Autonomous vehicles

Machine learning has revolutionized technology and continues to drive innovation across industries.`,

		"deep_learning.txt": `Deep Learning Explained

Deep learning is a specialized subset of machine learning that uses neural networks with multiple layers. These "deep" neural networks can automatically learn hierarchical representations of data.

Architecture Components:
- Input Layer: Receives raw data
- Hidden Layers: Extract features at different levels
- Output Layer: Produces final predictions

Popular Architectures:
1. CNNs (Convolutional Neural Networks) - For image processing
2. RNNs (Recurrent Neural Networks) - For sequential data
3. Transformers - For natural language processing
4. GANs (Generative Adversarial Networks) - For content generation

Deep learning has achieved state-of-the-art results in computer vision, NLP, and speech recognition.`,

		"python_ai.txt": `Python for AI and Machine Learning

Python has become the dominant language for AI and machine learning due to its simplicity and powerful libraries.

Essential Libraries:
- NumPy: Numerical computing
- Pandas: Data manipulation
- Scikit-learn: Machine learning algorithms
- TensorFlow: Deep learning framework
- PyTorch: Neural network library
- Keras: High-level neural networks API

Why Python for AI:
1. Easy to learn and read
2. Extensive ecosystem
3. Great community support
4. Excellent for prototyping
5. Integration capabilities

Python's versatility makes it the go-to choice for data scientists and ML engineers.`,

		"nlp_basics.txt": `Natural Language Processing Basics

Natural Language Processing (NLP) enables computers to understand, interpret, and generate human language in a valuable way.

Core NLP Tasks:
1. Tokenization - Breaking text into words/sentences
2. Part-of-Speech Tagging - Identifying word types
3. Named Entity Recognition - Extracting entities
4. Sentiment Analysis - Determining emotional tone
5. Machine Translation - Converting between languages

Modern Approaches:
- Word Embeddings (Word2Vec, GloVe)
- Transformers (BERT, GPT, T5)
- Large Language Models (LLMs)

NLP powers chatbots, virtual assistants, translation services, and content analysis tools.`,

		"computer_vision.txt": `Computer Vision Introduction

Computer Vision is a field of AI that trains computers to interpret and understand the visual world from digital images and videos.

Key Tasks:
1. Image Classification - Categorizing images
2. Object Detection - Locating objects in images
3. Semantic Segmentation - Pixel-level classification
4. Face Recognition - Identifying individuals
5. Pose Estimation - Detecting body positions

Techniques:
- Convolutional Neural Networks (CNNs)
- R-CNN and variants for object detection
- U-Net for segmentation
- GANs for image generation

Applications include autonomous vehicles, medical imaging, surveillance, and augmented reality.`,
	}

	for filename, content := range documents {
		if err := fs.WriteFile(ctx, filename, []byte(content)); err != nil {
			return err
		}
	}

	fmt.Printf("✅ Created %d sample documents\n", len(documents))
	return nil
}

func ingestDocuments(ctx context.Context, fs *fsxlocal.LocalFileSystem, docStore *document.DocumentStore) error {
	files, err := fs.List(ctx, "")
	if err != nil {
		return err
	}

	splitter := document.NewRecursiveTextSplitter(400, 40)
	totalChunks := 0

	for _, fileInfo := range files {
		if fileInfo.IsDir || !strings.HasSuffix(fileInfo.Name, ".txt") {
			continue
		}

		content, err := fs.ReadFile(ctx, fileInfo.Name)
		if err != nil {
			continue
		}

		doc := document.NewDocument(string(content)).
			WithID(fileInfo.Name).
			WithMetadata(document.MetadataSource, fileInfo.Name).
			WithMetadata(document.MetadataCategory, "ai-ml")

		chunks, err := splitter.Split(ctx, doc)
		if err != nil {
			continue
		}

		if err := docStore.AddDocuments(ctx, chunks); err != nil {
			continue
		}

		totalChunks += len(chunks)
		fmt.Printf("  📄 %s -> %d chunks\n", fileInfo.Name, len(chunks))
	}

	fmt.Printf("✅ Ingested %d chunks\n", totalChunks)
	return nil
}

// ============================================================================
// Search Examples
// ============================================================================

func searchExamples(ctx context.Context, docStore *document.DocumentStore) {
	queries := []string{
		"What is deep learning?",
		"Python libraries for machine learning",
		"NLP applications",
	}

	for i, query := range queries {
		fmt.Printf("\n  Query %d: %s\n", i+1, query)

		result, err := docStore.Search(ctx, document.SearchRequest{
			Query:    query,
			TopK:     2,
			MinScore: 0.6,
		})
		if err != nil {
			log.Printf("Search error: %v", err)
			continue
		}

		for j, doc := range result.Documents {
			fmt.Printf("    %d. Score: %.4f\n", j+1, result.Scores[j])
			fmt.Printf("       %s\n", truncate(doc.Content, 80))
		}
	}
}

// ============================================================================
// RAG Implementation
// ============================================================================

func performRAG(ctx context.Context, docStore *document.DocumentStore, llmClient *llm.Client, query string) {
	fmt.Printf("\n  Query: %s\n", query)

	// Retrieve
	retriever := document.NewRetriever(docStore).
		WithSearchType(document.SearchTypeMMR).
		WithTopK(3)

	docs, err := retriever.Retrieve(ctx, query)
	if err != nil {
		log.Printf("Retrieval error: %v", err)
		return
	}

	fmt.Printf("  Retrieved %d documents\n", len(docs))

	// Generate
	contextBuilder := document.NewContextBuilder().WithMaxLength(1500)
	context := contextBuilder.Build(docs)

	messages := []llm.Message{
		llm.NewSystemMessage("You are a helpful AI assistant. Answer based on the provided context."),
		llm.NewUserMessage(fmt.Sprintf("Context:\n%s\n\nQuestion: %s", context, query)),
	}

	response, err := llmClient.Chat(ctx, messages,
		llm.WithModel("gpt-4o-mini"),
		llm.WithTemperature(0.7),
	)
	if err != nil {
		log.Printf("LLM error: %v", err)
		return
	}

	fmt.Printf("\n  📝 Answer:\n%s\n", wrapText(response.Message.Content, 70))
}

// ============================================================================
// Advanced Features
// ============================================================================

func demonstrateFiltering(ctx context.Context, docStore *document.DocumentStore) {
	fmt.Println("\n  Filtered search example:")

	filter := vstore.NewFilter().
		AddMust("category", vstore.OpEqual, "ai-ml")

	result, err := docStore.Search(ctx, document.SearchRequest{
		Query:  "neural networks",
		TopK:   2,
		Filter: filter,
	})
	if err != nil {
		log.Printf("Filtered search error: %v", err)
		return
	}

	fmt.Printf("  Found %d filtered results\n", len(result.Documents))
}

func demonstrateNamespaces(ctx context.Context, vectorStore *vstore.Client, embedder document.Embedder) {
	fmt.Println("\n  Namespace example:")

	// Add to different namespace
	testDocs := []*document.Document{
		document.NewDocument("Test content in different namespace").
			WithID("test1").
			WithMetadata("category", "test"),
	}

	tempStore := document.NewDocumentStore(vectorStore, embedder).
		WithNamespace("test-namespace")

	if err := tempStore.AddDocuments(ctx, testDocs); err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("  ✅ Added documents to 'test-namespace'")

	// List namespaces
	if ns, err := vectorStore.ListNamespaces(ctx); err == nil {
		fmt.Printf("  Namespaces: %v\n", ns)
	}
}

// ============================================================================
// Utility Functions
// ============================================================================

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func wrapText(text string, width int) string {
	words := strings.Fields(text)
	var lines []string
	var currentLine string

	for _, word := range words {
		if len(currentLine)+len(word)+1 > width {
			if currentLine != "" {
				lines = append(lines, "  "+currentLine)
				currentLine = word
			}
		} else {
			if currentLine != "" {
				currentLine += " " + word
			} else {
				currentLine = word
			}
		}
	}
	if currentLine != "" {
		lines = append(lines, "  "+currentLine)
	}
	return strings.Join(lines, "\n")
}
