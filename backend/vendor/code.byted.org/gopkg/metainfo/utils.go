package metainfo

import (
	"context"

	"github.com/bytedance/gopkg/cloud/metainfo"
)

// HasMetaInfo detects whether the given context contains metainfo.
func HasMetaInfo(ctx context.Context) bool {
	return metainfo.HasMetaInfo(ctx)
}

// SetMetaInfoFromMap retrieves metainfo key-value pairs from the given map and sets then into the context.
// Only those keys with prefixes defined in this module would be used.
func SetMetaInfoFromMap(ctx context.Context, m map[string]string) context.Context {
	return metainfo.SetMetaInfoFromMap(ctx, m)
}

// SaveMetaInfoToMap set key-value pairs from ctx to m while filtering out transient-upstream data.
func SaveMetaInfoToMap(ctx context.Context, m map[string]string) {
	metainfo.SaveMetaInfoToMap(ctx, m)
}
