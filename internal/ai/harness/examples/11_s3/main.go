// Example 11_s3: a real agent whose files live in an S3 bucket instead of local
// disk, talking to multiple LLM providers through a router. This is the whole
// point of the swappable environment: the tools (Read, Write, Edit, List, Glob,
// Grep) are identical — only the FileSystem changes — and a single router
// Provider fans out to OpenAI or Anthropic based on agent.Model.
//
// There is no remote shell for S3, so we use builtins.Files (file tools only)
// and omit the Bash tool (which needs an exec.Executor).
//
// Run against real AWS (uses the default credential chain — env vars, shared
// config, SSO, IAM role). Provide either or both provider keys:
//
//	OPENAI_API_KEY=sk-... ANTHROPIC_API_KEY=sk-ant-... \
//	AWS_REGION=us-east-1 \
//	S3_BUCKET=my-bucket \
//	S3_PREFIX=agent-workspace \
//	go run ./internal/ai/harness/examples/11_s3 "list the files and summarize README.md"
//
// Or against a local MinIO / LocalStack for testing (no AWS account needed):
//
//	OPENAI_API_KEY=sk-... \
//	AWS_REGION=us-east-1 \
//	AWS_ACCESS_KEY_ID=minioadmin AWS_SECRET_ACCESS_KEY=minioadmin \
//	S3_ENDPOINT=http://localhost:9000 \
//	S3_BUCKET=my-bucket \
//	go run ./internal/ai/harness/examples/11_s3 "create notes.txt with a haiku about object storage"
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/fsxstore"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/anthropic"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/router"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool/builtins"
	"github.com/Abraxas-365/manifesto/internal/fsx/fsxs3"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		return fmt.Errorf("set S3_BUCKET")
	}

	// One router Provider fans out to whichever LLM providers have keys. There's
	// no hardcoded model->provider table: we declare the routes we want, and the
	// agent picks a provider by setting agent.Model.
	provider, defaultModel, err := buildRouter()
	if err != nil {
		return err
	}

	// Build a real S3 client from the standard AWS credential chain.
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		// Optional custom endpoint for MinIO / LocalStack. Path-style addressing
		// is required for those (bucket in the path, not the hostname).
		if ep := os.Getenv("S3_ENDPOINT"); ep != "" {
			o.BaseEndpoint = aws.String(ep)
			o.UsePathStyle = true
		}
	})

	// The one swap point: files live under s3://<bucket>/<prefix>/ instead of a
	// local directory. Everything below is identical to the local example.
	fs := fsxs3.NewS3FileSystem(s3Client, bucket, os.Getenv("S3_PREFIX"))

	// Storage-only: S3 has no shell, so we register the file tools and no Bash.
	// builtins.Files makes that impossible to get wrong — there is no executor to
	// diverge from the store.
	agent := harness.New(provider, builtins.Files(fsxstore.New(fs)))
	agent.System = "You are a coding assistant. The files you work with live in an S3 bucket, " +
		"exposed through the standard file tools. Use them to inspect and edit files."
	agent.Model = defaultModel

	// Optional override: MODEL=claude-sonnet-4-20250514 routes the same agent to
	// Anthropic without touching any other code.
	if m := os.Getenv("MODEL"); m != "" {
		agent.Model = m
	}

	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		prompt = "List the files in the workspace and describe what you find."
	}

	answer, err := agent.Run(ctx, prompt)
	if err != nil {
		return err
	}
	fmt.Printf("[%s] %s\n", agent.Model, answer)
	return nil
}

// buildRouter wires a router Provider from whatever provider keys are present
// and returns a sensible default model. At least one key is required.
func buildRouter() (llm.Provider, string, error) {
	openaiKey := os.Getenv("OPENAI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")

	r := router.New()
	defaultModel := ""

	if openaiKey != "" {
		r.HandlePattern("gpt-*", openai.New(openaiKey))
		r.HandlePattern("o1-*", openai.New(openaiKey))
		r.HandlePattern("o3-*", openai.New(openaiKey))
		defaultModel = "gpt-4o"
	}
	if anthropicKey != "" {
		// Prompt caching is a provider-level option; harmless if unused.
		r.HandlePattern("claude-*", anthropic.NewWithOptions(anthropicKey, []anthropic.Option{
			anthropic.WithPromptCaching(),
		}))
		if defaultModel == "" {
			defaultModel = "claude-sonnet-4-20250514"
		}
	}

	if defaultModel == "" {
		return nil, "", fmt.Errorf("set OPENAI_API_KEY and/or ANTHROPIC_API_KEY")
	}
	return r, defaultModel, nil
}

