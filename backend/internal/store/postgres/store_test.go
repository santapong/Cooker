package postgres

import (
	"context"
	"database/sql"
	"net"
	"testing"
	"time"
)

func TestPingWithBackoff_BudgetExhausted(t *testing.T) {
	// Reserve a port then close so connections refuse fast.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	db, err := sql.Open("postgres", "postgres://x:y@"+addr+"/db?sslmode=disable")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	prevBudget, prevInit, prevMax := pingBudget, pingInitialDelay, pingMaxDelay
	pingBudget, pingInitialDelay, pingMaxDelay = 300*time.Millisecond, 50*time.Millisecond, 100*time.Millisecond
	defer func() { pingBudget, pingInitialDelay, pingMaxDelay = prevBudget, prevInit, prevMax }()

	start := time.Now()
	err = pingWithBackoff(context.Background(), db)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected ping budget to be exhausted")
	}
	if elapsed < 200*time.Millisecond {
		t.Fatalf("loop returned too fast (%s); should retry until budget expires", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("loop ran far too long (%s)", elapsed)
	}
}

func TestPingWithBackoff_CancelledCtx(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	db, err := sql.Open("postgres", "postgres://x:y@"+addr+"/db?sslmode=disable")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	prevBudget := pingBudget
	pingBudget = 10 * time.Second
	defer func() { pingBudget = prevBudget }()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	if err := pingWithBackoff(ctx, db); err == nil {
		t.Fatal("expected error from cancelled ctx")
	}
}
