/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controller

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"k8s.io/client-go/util/retry"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/pool"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs/pgbouncer"
)

// PgBouncerInstanceInterface the public interface for a PgBouncer instance,
// implementations should be thread safe
type PgBouncerInstanceInterface interface {
	Paused() bool
	Pause() error
	Resume() error
}

// NewPgBouncerInstance initializes a new pgBouncerInstance
func NewPgBouncerInstance() PgBouncerInstanceInterface {
	dsn := fmt.Sprintf(
		"host=%s port=%v user=%s sslmode=disable",
		pgbouncer.PgBouncerSocketDir,
		pgbouncer.PgBouncerPort,
		pgbouncer.PgBouncerAdminUser,
	)

	return &pgBouncerInstance{
		mu:     &sync.RWMutex{},
		paused: false,
		pool:   pool.NewConnectionPool(dsn),
	}
}

type pgBouncerInstance struct {
	// The following two fields are used to keep track of
	// pgbouncer being paused or not
	mu     *sync.RWMutex
	paused bool

	// This is the connection pool used to connect to pgbouncer
	// using the administrative user and the administrative database
	pool *pool.ConnectionPool
}

// Paused returns whether the pgbouncerInstance is paused or not, thread safe
func (p *pgBouncerInstance) Paused() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.paused
}

// Pause the instance, thread safe
func (p *pgBouncerInstance) Pause() error {
	// First step: connect to the pgbouncer administrative database
	db, err := p.pool.Connection("pgbouncer")
	if err != nil {
		return fmt.Errorf("while connecting to pgbouncer database locally: %w", err)
	}

	// Second step: pause pgbouncer
	//
	// We are retrying the PAUSE query since we need to wait for
	// pgbouncer to be really up and the user could have created
	// a pooler which is paused from the start.
	err = retry.OnError(retry.DefaultBackoff, func(err error) bool {
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		return true
	}, func() error {
		_, err = db.Exec("PAUSE")
		return err
	})
	if err != nil {
		return err
	}

	// Third step: keep track of pgbouncer being paused
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paused = true

	return nil
}

// Resume the instance, thread safe
func (p *pgBouncerInstance) Resume() error {
	// First step: connect to the pgbouncer administrative database
	db, err := p.pool.Connection("pgbouncer")
	if err != nil {
		return fmt.Errorf("while connecting to pgbouncer database locally: %w", err)
	}

	// Second step: resume pgbouncer
	_, err = db.Exec("RESUME")
	if err != nil {
		return fmt.Errorf("while resuming instance: %w", err)
	}

	// Third step: keep track of pgbouncer being resumed
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paused = false

	return nil
}