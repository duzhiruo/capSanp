package vectorstore

import (
	"crypto/sha1"
	"fmt"
)

// PointUUID 将业务 ID（如 shot_xxx）转为 Qdrant 可用的确定性 UUID。
func PointUUID(id string) string {
	h := sha1.Sum([]byte("capsnap:" + id))
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
