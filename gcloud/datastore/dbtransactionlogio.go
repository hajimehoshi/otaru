package datastore

import (
	"fmt"
	"log"
	"sync"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/cloud"
	"google.golang.org/cloud/datastore"

	"github.com/nyaxt/otaru/btncrypt"
	"github.com/nyaxt/otaru/gcloud/auth"
	"github.com/nyaxt/otaru/inodedb"
	"github.com/nyaxt/otaru/util"
)

type DBTransactionLogIO struct {
	projectName string
	rootKey     *datastore.Key
	c           btncrypt.Cipher
	clisrc      auth.ClientSource

	mu        sync.Mutex
	nextbatch []inodedb.DBTransaction

	syncer *util.PeriodicRunner
}

const (
	kindTransaction = "OtaruINodeDBTx"
)

var _ = inodedb.DBTransactionLogIO(&DBTransactionLogIO{})

func NewDBTransactionLogIO(projectName, rootKeyStr string, c btncrypt.Cipher, clisrc auth.ClientSource) (*DBTransactionLogIO, error) {
	txio := &DBTransactionLogIO{
		projectName: projectName,
		c:           c,
		clisrc:      clisrc,
		nextbatch:   make([]inodedb.DBTransaction, 0),
	}
	ctx := txio.getContext()
	txio.rootKey = datastore.NewKey(ctx, kindTransaction, rootKeyStr, 0, nil)
	txio.syncer = util.NewSyncScheduler(txio, 300*time.Millisecond)

	return txio, nil
}

func (txio *DBTransactionLogIO) getContext() context.Context {
	return cloud.NewContext(txio.projectName, txio.clisrc(context.TODO()))
}

type storedbtx struct {
	TxID    int64
	OpsJSON []byte
}

func encode(c btncrypt.Cipher, tx inodedb.DBTransaction) (*storedbtx, error) {
	jsonops, err := inodedb.EncodeDBOperationsToJson(tx.Ops)
	if err != nil {
		return nil, fmt.Errorf("Failed to encode dbtx: %v", err)
	}

	env, err := btncrypt.Encrypt(c, jsonops)
	if err != nil {
		return nil, fmt.Errorf("Failed to decrypt OpsJSON: %v", err)
	}

	return &storedbtx{TxID: int64(tx.TxID), OpsJSON: env}, nil
}

func decode(c btncrypt.Cipher, stx *storedbtx) (inodedb.DBTransaction, error) {
	jsonop, err := btncrypt.Decrypt(c, stx.OpsJSON, len(stx.OpsJSON)-c.FrameOverhead())
	if err != nil {
		return inodedb.DBTransaction{}, fmt.Errorf("Failed to decrypt OpsJSON: %v", err)
	}

	ops, err := inodedb.DecodeDBOperationsFromJson(jsonop)
	if err != nil {
		return inodedb.DBTransaction{}, err
	}

	return inodedb.DBTransaction{TxID: inodedb.TxID(stx.TxID), Ops: ops}, nil
}

func (txio *DBTransactionLogIO) AppendTransaction(tx inodedb.DBTransaction) error {
	txio.mu.Lock()
	defer txio.mu.Unlock()

	txio.nextbatch = append(txio.nextbatch, tx)
	return nil
}

func (txio *DBTransactionLogIO) Sync() error {
	txio.mu.Lock()
	batch := txio.nextbatch
	txio.nextbatch = make([]inodedb.DBTransaction, 0)
	txio.mu.Unlock()

	if len(batch) == 0 {
		return nil
	}

	ctx := txio.getContext()
	keys := make([]*datastore.Key, 0, len(batch))
	stxs := make([]*storedbtx, 0, len(batch))
	for _, tx := range batch {
		keys = append(keys, datastore.NewKey(ctx, kindTransaction, "", int64(tx.TxID), txio.rootKey))
		stx, err := encode(txio.c, tx)
		if err != nil {
			return err
		}
		stxs = append(stxs, stx)
	}

	if _, err := datastore.PutMulti(ctx, keys, stxs); err != nil {
		return err
	}
	log.Printf("Committed %d txs", len(stxs))
	return nil
}

func (txio *DBTransactionLogIO) QueryTransactions(minID inodedb.TxID) ([]inodedb.DBTransaction, error) {
	start := time.Now()
	result := []inodedb.DBTransaction{}

	txio.mu.Lock()
	for _, tx := range txio.nextbatch {
		if tx.TxID >= minID {
			result = append(result, tx)
		}
	}
	txio.mu.Unlock()

	ctx := txio.getContext()

	q := datastore.NewQuery(kindTransaction).Ancestor(txio.rootKey).Filter("TxID >=", int64(minID)).Order("TxID")
	it := q.Run(ctx)
	for {
		var stx storedbtx
		_, err := it.Next(&stx)
		if err != nil {
			if err == datastore.Done {
				break
			}
			return []inodedb.DBTransaction{}, err
		}

		tx, err := decode(txio.c, &stx)
		if err != nil {
			return []inodedb.DBTransaction{}, err
		}

		result = append(result, tx)
	}
	log.Printf("QueryTransactions(%v) took %s", minID, time.Since(start))
	return result, nil
}

func (txio *DBTransactionLogIO) DeleteTransactions(smallerThanID inodedb.TxID) error {
	start := time.Now()

	txio.mu.Lock()
	batch := make([]inodedb.DBTransaction, 0, len(txio.nextbatch))
	for _, tx := range txio.nextbatch {
		if tx.TxID < smallerThanID {
			continue
		}
		batch = append(batch, tx)
	}
	txio.nextbatch = batch
	txio.mu.Unlock()

	ctx := txio.getContext()

	keys := []*datastore.Key{}
	q := datastore.NewQuery(kindTransaction).Ancestor(txio.rootKey).Filter("TxID <", int64(smallerThanID)).KeysOnly()
	it := q.Run(ctx)
	for {
		k, err := it.Next(nil)
		if err != nil {
			if err == datastore.Done {
				break
			}
			return err
		}

		keys = append(keys, k)
	}

	log.Printf("keys to delete: %v", keys)
	if err := datastore.DeleteMulti(ctx, keys); err != nil {
		return err
	}

	log.Printf("DeleteTransactions(%v) took %s", smallerThanID, time.Since(start))
	return nil
}

func (txio *DBTransactionLogIO) DeleteAllTransactions() error {
	return txio.DeleteTransactions(inodedb.LatestVersion)
}
