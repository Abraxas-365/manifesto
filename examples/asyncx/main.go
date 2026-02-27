package main

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/asyncx"
)

func main() {
	ctx := context.Background()

	fmt.Println("Asyncx Examples")
	fmt.Println(strings.Repeat("=", 60))

	// ========================================================================
	// 1. Future — run a function asynchronously and await its result
	// ========================================================================

	fmt.Println("\n--- Future (Run + Await) ---")

	future := asyncx.Run(func() (string, error) {
		time.Sleep(100 * time.Millisecond) // simulate work
		return "computed result", nil
	})

	// Do other work here while the future runs...
	fmt.Println("Doing other work while future runs...")

	result, err := future.Await()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
	fmt.Printf("Future result: %s\n", result)

	// ========================================================================
	// 2. Do / DoCtx — fire-and-forget goroutines
	// ========================================================================

	fmt.Println("\n--- Do (fire-and-forget) ---")

	done := make(chan struct{})
	asyncx.Do(func() {
		fmt.Println("Background task executed!")
		close(done)
	})
	<-done

	// DoCtx respects context cancellation
	asyncx.DoCtx(ctx, func(ctx context.Context) {
		fmt.Println("Context-aware background task executed!")
	})

	time.Sleep(50 * time.Millisecond) // let it finish

	// ========================================================================
	// 3. All — run multiple functions concurrently, short-circuit on error
	// ========================================================================

	fmt.Println("\n--- All (concurrent, fail-fast) ---")

	results, err := asyncx.All(ctx,
		func(ctx context.Context) (string, error) {
			time.Sleep(50 * time.Millisecond)
			return "from API A", nil
		},
		func(ctx context.Context) (string, error) {
			time.Sleep(30 * time.Millisecond)
			return "from API B", nil
		},
		func(ctx context.Context) (string, error) {
			time.Sleep(40 * time.Millisecond)
			return "from API C", nil
		},
	)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("All results: %v\n", results)
	}

	// ========================================================================
	// 4. AllSettled — run all, collect results regardless of errors
	// ========================================================================

	fmt.Println("\n--- AllSettled (collect all, even errors) ---")

	settled := asyncx.AllSettled(ctx,
		func(ctx context.Context) (int, error) {
			return 42, nil
		},
		func(ctx context.Context) (int, error) {
			return 0, fmt.Errorf("something went wrong")
		},
		func(ctx context.Context) (int, error) {
			return 100, nil
		},
	)

	for i, r := range settled {
		if r.OK() {
			fmt.Printf("  Task %d: value=%d\n", i, r.Value)
		} else {
			fmt.Printf("  Task %d: error=%v\n", i, r.Err)
		}
	}

	// ========================================================================
	// 5. Race — return the first result
	// ========================================================================

	fmt.Println("\n--- Race (first to finish wins) ---")

	winner, err := asyncx.Race(ctx,
		func(ctx context.Context) (string, error) {
			time.Sleep(100 * time.Millisecond)
			return "slow server", nil
		},
		func(ctx context.Context) (string, error) {
			time.Sleep(10 * time.Millisecond)
			return "fast server", nil
		},
	)
	fmt.Printf("Winner: %s\n", winner)

	// ========================================================================
	// 6. Map — transform a slice concurrently
	// ========================================================================

	fmt.Println("\n--- Map (concurrent transform) ---")

	urls := []string{"user/1", "user/2", "user/3", "user/4"}

	userNames, err := asyncx.Map(ctx, urls, func(ctx context.Context, url string) (string, error) {
		// simulate API call
		time.Sleep(20 * time.Millisecond)
		return fmt.Sprintf("User<%s>", url), nil
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Users: %v\n", userNames)
	}

	// ========================================================================
	// 7. Pool — bounded concurrency with worker pool
	// ========================================================================

	fmt.Println("\n--- Pool (bounded workers) ---")

	items := []int{1, 2, 3, 4, 5, 6, 7, 8}

	squared, err := asyncx.Pool(ctx, 3, items, func(ctx context.Context, n int) (int, error) {
		time.Sleep(10 * time.Millisecond) // simulate work
		return n * n, nil
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Squared (3 workers): %v\n", squared)
	}

	// ========================================================================
	// 8. ForEach — apply function to each item concurrently
	// ========================================================================

	fmt.Println("\n--- ForEach (concurrent side effects) ---")

	notifications := []string{"user@a.com", "user@b.com", "user@c.com"}

	err = asyncx.ForEach(ctx, notifications, func(ctx context.Context, email string) error {
		fmt.Printf("  Sending notification to %s\n", email)
		return nil
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// ========================================================================
	// 9. Retry / RetryWithBackoff — automatic retries
	// ========================================================================

	fmt.Println("\n--- Retry ---")

	attempt := 0
	val, err := asyncx.Retry(ctx, 3, func(ctx context.Context) (string, error) {
		attempt++
		if attempt < 3 {
			return "", fmt.Errorf("attempt %d failed", attempt)
		}
		return "success on attempt 3", nil
	})
	fmt.Printf("Retry result: %s (after %d attempts)\n", val, attempt)

	fmt.Println("\n--- RetryWithBackoff ---")

	attempt = 0
	val, err = asyncx.RetryWithBackoff(ctx, 3, 10*time.Millisecond, func(ctx context.Context) (string, error) {
		attempt++
		if attempt < 2 {
			return "", fmt.Errorf("attempt %d failed", attempt)
		}
		return "success with backoff", nil
	})
	fmt.Printf("RetryWithBackoff result: %s\n", val)

	// ========================================================================
	// 10. WithTimeout — run with a deadline
	// ========================================================================

	fmt.Println("\n--- WithTimeout ---")

	val, err = asyncx.WithTimeout(ctx, 200*time.Millisecond, func(ctx context.Context) (string, error) {
		time.Sleep(50 * time.Millisecond)
		return "completed in time", nil
	})
	fmt.Printf("WithTimeout: %s\n", val)

	// Timeout case
	_, err = asyncx.WithTimeout(ctx, 10*time.Millisecond, func(ctx context.Context) (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "too late", nil
	})
	fmt.Printf("WithTimeout (expired): err=%v\n", err)

	// ========================================================================
	// 11. Once — execute exactly once, cache result
	// ========================================================================

	fmt.Println("\n--- Once (singleton) ---")

	initDB := asyncx.Once(func() (string, error) {
		fmt.Println("  Initializing DB connection...")
		return "db-connection-pool", nil
	})

	conn1, _ := initDB()
	conn2, _ := initDB() // uses cached result
	conn3, _ := initDB() // uses cached result
	fmt.Printf("All same connection: %s, %s, %s\n", conn1, conn2, conn3)

	// ========================================================================
	// 12. Debounced — only execute after quiet period
	// ========================================================================

	fmt.Println("\n--- Debounced ---")

	debounceCount := 0
	debounced := asyncx.Debounced(50*time.Millisecond, func() {
		debounceCount++
	})

	// Rapid calls — only the last one should fire
	for i := 0; i < 5; i++ {
		debounced()
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	fmt.Printf("Debounced: called %d time(s) despite 5 invocations\n", debounceCount)

	// ========================================================================
	// 13. Throttled — execute at most once per interval
	// ========================================================================

	fmt.Println("\n--- Throttled ---")

	throttleCount := 0
	throttled := asyncx.Throttled(50*time.Millisecond, func() {
		throttleCount++
	})

	for i := 0; i < 10; i++ {
		throttled()
		time.Sleep(15 * time.Millisecond)
	}
	fmt.Printf("Throttled: executed %d time(s) out of 10 calls\n", throttleCount)

	// ========================================================================
	// Real-world pattern: fetch multiple APIs in parallel with timeout
	// ========================================================================

	fmt.Println("\n--- Real-world: parallel API calls with timeout ---")

	type UserProfile struct {
		Name  string
		Score int
	}

	profile, err := asyncx.WithTimeout(ctx, 500*time.Millisecond, func(ctx context.Context) (UserProfile, error) {
		results, err := asyncx.All(ctx,
			func(ctx context.Context) (string, error) {
				time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
				return "Alice", nil
			},
			func(ctx context.Context) (string, error) {
				time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
				return "95", nil
			},
		)
		if err != nil {
			return UserProfile{}, err
		}
		return UserProfile{Name: results[0], Score: 95}, nil
	})

	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Profile: %+v\n", profile)
	}

	fmt.Println("\nDone!")
}
