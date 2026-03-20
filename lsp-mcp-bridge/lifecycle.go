package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"
)

type bridgeServer interface {
	Start(addr string) error
	Shutdown(ctx context.Context) error
}

type managerShutdowner interface {
	Shutdown(ctx context.Context)
}

func runBridge(ctx context.Context, srv bridgeServer, mgr managerShutdowner, addr string, logger *log.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(addr)
	}()

	select {
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Println("shutting down")

		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = srv.Shutdown(shutCtx)
		mgr.Shutdown(shutCtx)

		err := <-errCh
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
