package storage

import (
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestSQLiteStorage_ConcurrentReadWriteRestoreCount(t *testing.T) {
	db, err := NewSQLiteStorage("et.db?_journal_mode=WAL&mode=memory&_sync=1&_txlock=immediate")
	if err != nil {
		t.Fatalf("Failed to create SQLiteStorage: %v", err)
	}
	defer db.Close()

	userID := uint64(1)
	channelID := uint64(1)
	concurrency := 50

	wg := sync.WaitGroup{}
	wg.Add(concurrency*2)
	wg2 := sync.WaitGroup{}
	wg2.Add(concurrency*2)
	entry := time.Now()
	results := make(chan error, concurrency*2)
	for i := 0; i < concurrency; i++ {
		go func() {
			wg.Done()
			wg.Wait()
			defer wg2.Done()
			for j := 0; j < 1000; j++ {
				_, err := db.IncreaseQuotaUsage(userID, channelID, 1)
				if err != nil {
					results <- err
				}
			}
		}()
	}
	for i := 0; i < concurrency; i++ {
		go func() {
			wg.Done()
			wg.Wait()
			defer wg2.Done()
			for j := 0; j < 1000; j++ {
				err := db.ResetQuotaUsage(userID, channelID)
				if err != nil {
					results <- err
				}
			}
		}()
	}
	wg2.Wait()
	close(results)

	for err := range results {
		if err != nil {
			t.Fatalf("Failed to reset restore count: %v", err)
		}
	}
	elapsed := time.Since(entry)
	t.Logf("Elapsed: %v", elapsed)
}

func TestBeginingOfTheDay (t *testing.T) {
	t.Logf("NOW: %v", time.Now())
	t.Logf("NOW UTC: %v", time.Now().UTC())
	taipeiTime := time.Now().UTC().Add(time.Hour*8).Truncate(time.Hour * 24)
	t.Logf("Begining of the day (TPE): %v", taipeiTime)
}
