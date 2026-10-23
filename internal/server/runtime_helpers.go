package server

import (
	"context"
	"github.com/heroiclabs/nakama-common/runtime"
)

const opAction int64 = 1
const opSnapshot int64 = 2
const opError int64 = 3

func userID(ctx context.Context) (string, error) {
	id, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok || id == "" {
		return "", runtime.NewError("Authentication required", 16)
	}
	return id, nil
}
