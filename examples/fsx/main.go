package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/fsx"
	"github.com/Abraxas-365/manifesto/pkg/fsx/fsxlocal"
)

func main() {
	ctx := context.Background()

	fmt.Println("FSX Examples — Unified File System Abstraction")
	fmt.Println(strings.Repeat("=", 60))

	// ========================================================================
	// 1. Create a local file system
	// ========================================================================
	// fsxlocal implements the fsx.FileSystem interface.
	// The same code works with fsxs3.S3FileSystem for S3 storage.

	fmt.Println("\n--- Setup ---")

	tmpDir, err := os.MkdirTemp("", "fsx-example-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	fs, err := fsxlocal.NewLocalFileSystem(tmpDir)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Base path: %s\n", fs.GetBasePath())

	// ========================================================================
	// 2. Write files
	// ========================================================================

	fmt.Println("\n--- Write Files ---")

	files := map[string]string{
		"hello.txt":          "Hello, World!",
		"notes/todo.txt":     "Buy groceries\nFinish project\nRead a book",
		"notes/ideas.txt":    "Build a CLI tool\nLearn Rust\nContribute to OSS",
		"data/config.json":   `{"debug": true, "port": 8080}`,
	}

	for path, content := range files {
		if err := fs.WriteFile(ctx, path, []byte(content)); err != nil {
			log.Fatalf("Failed to write %s: %v", path, err)
		}
		fmt.Printf("  Wrote %s (%d bytes)\n", path, len(content))
	}

	// ========================================================================
	// 3. Read files
	// ========================================================================

	fmt.Println("\n--- Read Files ---")

	data, err := fs.ReadFile(ctx, "hello.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  hello.txt: %s\n", string(data))

	data, err = fs.ReadFile(ctx, "data/config.json")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  data/config.json: %s\n", string(data))

	// ========================================================================
	// 4. Check file existence
	// ========================================================================

	fmt.Println("\n--- Exists ---")

	exists, _ := fs.Exists(ctx, "hello.txt")
	fmt.Printf("  hello.txt exists: %v\n", exists)

	exists, _ = fs.Exists(ctx, "missing.txt")
	fmt.Printf("  missing.txt exists: %v\n", exists)

	// ========================================================================
	// 5. Get file info (Stat)
	// ========================================================================

	fmt.Println("\n--- Stat ---")

	info, err := fs.Stat(ctx, "hello.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  Name: %s\n", info.Name)
	fmt.Printf("  Size: %d bytes\n", info.Size)
	fmt.Printf("  IsDir: %v\n", info.IsDir)
	fmt.Printf("  ModTime: %v\n", info.ModTime.Format("2006-01-02 15:04:05"))

	// ========================================================================
	// 6. List directory contents
	// ========================================================================

	fmt.Println("\n--- List (root) ---")

	entries, err := fs.List(ctx, "")
	if err != nil {
		log.Fatal(err)
	}
	for _, entry := range entries {
		kind := "FILE"
		if entry.IsDir {
			kind = "DIR "
		}
		fmt.Printf("  [%s] %s (%d bytes)\n", kind, entry.Name, entry.Size)
	}

	fmt.Println("\n--- List (notes/) ---")

	entries, err = fs.List(ctx, "notes")
	if err != nil {
		log.Fatal(err)
	}
	for _, entry := range entries {
		fmt.Printf("  %s (%d bytes)\n", entry.Name, entry.Size)
	}

	// ========================================================================
	// 7. Path operations
	// ========================================================================

	fmt.Println("\n--- Join (path operations) ---")

	joined := fs.Join("data", "nested", "file.txt")
	fmt.Printf("  Join(\"data\", \"nested\", \"file.txt\") = %s\n", joined)

	// ========================================================================
	// 8. Create directories
	// ========================================================================

	fmt.Println("\n--- CreateDir ---")

	if err := fs.CreateDir(ctx, "logs/2024/01"); err != nil {
		log.Fatal(err)
	}
	fmt.Println("  Created logs/2024/01/")

	exists, _ = fs.Exists(ctx, "logs/2024/01")
	fmt.Printf("  logs/2024/01 exists: %v\n", exists)

	// ========================================================================
	// 9. Delete files
	// ========================================================================

	fmt.Println("\n--- Delete ---")

	if err := fs.DeleteFile(ctx, "hello.txt"); err != nil {
		log.Fatal(err)
	}
	fmt.Println("  Deleted hello.txt")

	exists, _ = fs.Exists(ctx, "hello.txt")
	fmt.Printf("  hello.txt exists: %v\n", exists)

	// Delete directory recursively
	if err := fs.DeleteDir(ctx, "notes", true); err != nil {
		log.Fatal(err)
	}
	fmt.Println("  Deleted notes/ (recursive)")

	// ========================================================================
	// 10. Streaming read/write
	// ========================================================================

	fmt.Println("\n--- Streaming ---")

	// Write using a reader
	reader := strings.NewReader("streamed content here")
	if err := fs.WriteFileStream(ctx, "streamed.txt", reader); err != nil {
		log.Fatal(err)
	}
	fmt.Println("  Wrote streamed.txt via WriteFileStream")

	// Read using a stream
	rc, err := fs.ReadFileStream(ctx, "streamed.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer rc.Close()

	buf := make([]byte, 1024)
	n, _ := rc.Read(buf)
	fmt.Printf("  ReadFileStream: %s\n", string(buf[:n]))

	// ========================================================================
	// 11. Using the interface — code that works with any backend
	// ========================================================================

	fmt.Println("\n--- Interface polymorphism ---")

	// This function accepts any fsx.FileReader — works with local, S3, etc.
	printFileContents(ctx, fs, "data/config.json")

	// ========================================================================
	// S3 usage (for reference)
	// ========================================================================

	fmt.Println("\n--- S3 Example (code only, not executed) ---")
	fmt.Println("  // s3Client := s3.NewFromConfig(cfg)")
	fmt.Println("  // s3FS := fsxs3.NewS3FileSystem(s3Client, \"my-bucket\", \"prefix/\")")
	fmt.Println("  // s3FS.WriteFile(ctx, \"file.txt\", data)")
	fmt.Println("  // s3FS.ReadFile(ctx, \"file.txt\")")
	fmt.Println("  //")
	fmt.Println("  // Presigned URLs (S3 only):")
	fmt.Println("  // url, _ := s3FS.GetPresignedDownloadURL(ctx, \"file.txt\", 15*time.Minute)")
	fmt.Println("  // url, _ := s3FS.GetPresignedUploadURL(ctx, \"file.txt\", 15*time.Minute)")

	fmt.Println("\nDone!")
}

// printFileContents demonstrates using the fsx.FileReader interface.
// This function works with any backend: local, S3, or custom implementations.
func printFileContents(ctx context.Context, reader fsx.FileReader, path string) {
	data, err := reader.ReadFile(ctx, path)
	if err != nil {
		fmt.Printf("  Error reading %s: %v\n", path, err)
		return
	}
	fmt.Printf("  [%s]: %s\n", path, string(data))
}
