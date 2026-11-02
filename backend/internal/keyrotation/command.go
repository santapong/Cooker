package keyrotation

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"time"

	_ "github.com/lib/pq"
)

// RunCommand accepts secrets only through the provided private input stream.
// Output contains fixed errors and audit metadata, never connection details.
func RunCommand(ctx context.Context, args []string, input io.Reader, output io.Writer) int {
	if len(args) != 1 || (args[0] != "inventory" && args[0] != "rotate" && args[0] != "status" && args[0] != "restore") {
		json.NewEncoder(output).Encode(map[string]string{"error": "usage: cooker key-rotation inventory|rotate|status|restore; private JSON on stdin"})
		return 2
	}
	// The private-input read is part of the same deadline as database work.
	// In particular, SIGTERM must not leave a CLI blocked on an open stdin pipe.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	data, err := readPrivateInput(ctx, input)
	defer clear(data)
	if err != nil || len(data) > 65536 {
		json.NewEncoder(output).Encode(map[string]string{"error": ErrInput.Error()})
		return 2
	}
	var in Input
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || in.DatabaseURL == "" {
		json.NewEncoder(output).Encode(map[string]string{"error": ErrInput.Error()})
		return 2
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		json.NewEncoder(output).Encode(map[string]string{"error": ErrInput.Error()})
		return 2
	}
	defer func() { in.DatabaseURL = ""; in.OldKey = ""; in.NewKey = "" }()
	db, err := sql.Open("postgres", in.DatabaseURL)
	if err != nil {
		json.NewEncoder(output).Encode(map[string]string{"error": ErrDatabase.Error()})
		return 1
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	r, err := Execute(ctx, db, args[0], in)
	if err != nil {
		json.NewEncoder(output).Encode(struct {
			Report Report `json:"report"`
			Error  string `json:"error"`
		}{r, err.Error()})
		return 1
	}
	if json.NewEncoder(output).Encode(r) != nil {
		return 1
	}
	return 0
}

// readPrivateInput releases owned stdin on cancellation. Arbitrary Reader
// implementations may not offer an interruptible Read; their abandoned bytes
// are cleared by the reader goroutine rather than handed back to a caller.
func readPrivateInput(ctx context.Context, input io.Reader) ([]byte, error) {
	if input == nil || ctx.Err() != nil {
		return nil, ErrInput
	}
	type result struct {
		data []byte
		err  error
	}
	ready := make(chan result)
	go func() {
		data, err := io.ReadAll(io.LimitReader(input, 65537))
		select {
		case ready <- result{data, err}:
		case <-ctx.Done():
			clear(data)
		}
	}()
	select {
	case r := <-ready:
		if ctx.Err() != nil {
			clear(r.data)
			return nil, ErrInput
		}
		return r.data, r.err
	case <-ctx.Done():
		if closer, ok := input.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, ErrInput
	}
}
