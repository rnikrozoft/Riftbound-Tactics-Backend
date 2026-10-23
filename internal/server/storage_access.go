package server

import (
	"context"
	"database/sql"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
	"strings"
)

// Object permissions protect existing records. Before hooks also reserve keys
// before their first server write, so a client cannot seed forged game data.
// Runtime StorageWrite/MultiUpdate calls do not pass through client API hooks.
func beforeStorageWrite(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.WriteStorageObjectsRequest) (*api.WriteStorageObjectsRequest, error) {
	for _, object := range in.GetObjects() {
		if strings.HasPrefix(object.GetCollection(), "riftbound_") {
			return nil, runtime.NewError("Game storage is server managed", 7)
		}
	}
	return in, nil
}

func beforeStorageDelete(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.DeleteStorageObjectsRequest) (*api.DeleteStorageObjectsRequest, error) {
	for _, object := range in.GetObjectIds() {
		if strings.HasPrefix(object.GetCollection(), "riftbound_") {
			return nil, runtime.NewError("Game storage is server managed", 7)
		}
	}
	return in, nil
}

func registerStorageAccess(i runtime.Initializer) error {
	if err := i.RegisterBeforeWriteStorageObjects(beforeStorageWrite); err != nil {
		return err
	}
	return i.RegisterBeforeDeleteStorageObjects(beforeStorageDelete)
}
