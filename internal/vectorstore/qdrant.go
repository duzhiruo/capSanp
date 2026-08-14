package vectorstore

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
)

type Qdrant struct {
	client     *qdrant.Client
	collection string
	dimension  uint64
}

func NewQdrant(host string, port int, collection string, dimension int) (*Qdrant, error) {
	client, err := qdrant.NewClient(&qdrant.Config{
		Host: host,
		Port: port,
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant 连接失败: %w", err)
	}
	return &Qdrant{client: client, collection: collection, dimension: uint64(dimension)}, nil
}

func (q *Qdrant) Close() error {
	return q.client.Close()
}

func (q *Qdrant) EnsureCollection(ctx context.Context) error {
	exists, err := q.client.CollectionExists(ctx, q.collection)
	if err != nil {
		return fmt.Errorf("检查 collection 失败: %w", err)
	}
	if exists {
		return nil
	}
	return q.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: q.collection,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     q.dimension,
			Distance: qdrant.Distance_Cosine,
		}),
	})
}

func (q *Qdrant) Upsert(ctx context.Context, id string, vector []float32, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["screenshot_id"]; !ok {
		payload["screenshot_id"] = id
	}
	wait := true
	_, err := q.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: q.collection,
		Wait:           &wait,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewID(PointUUID(id)),
				Vectors: qdrant.NewVectors(vector...),
				Payload: qdrant.NewValueMap(payload),
			},
		},
	})
	return err
}

func (q *Qdrant) Search(ctx context.Context, vector []float32, limit int, filter map[string]any) ([]SearchResult, error) {
	req := &qdrant.QueryPoints{
		CollectionName: q.collection,
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(uint64(limit)),
		WithPayload:    qdrant.NewWithPayload(true),
	}
	if deviceID, ok := filter["device_id"].(string); ok && deviceID != "" {
		req.Filter = &qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatchKeyword("device_id", deviceID),
			},
		}
	}

	points, err := q.client.Query(ctx, req)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(points))
	for _, p := range points {
		payload := make(map[string]any)
		for k, v := range p.GetPayload() {
			payload[k] = valueToAny(v)
		}
		id, _ := payload["screenshot_id"].(string)
		if id == "" {
			id = p.GetId().GetUuid()
		}
		results = append(results, SearchResult{
			ID:      id,
			Score:   p.GetScore(),
			Payload: payload,
		})
	}
	return results, nil
}

func (q *Qdrant) Delete(ctx context.Context, id string) error {
	wait := true
	_, err := q.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: q.collection,
		Wait:           &wait,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Points{
				Points: &qdrant.PointsIdsList{
					Ids: []*qdrant.PointId{qdrant.NewID(PointUUID(id))},
				},
			},
		},
	})
	return err
}

func valueToAny(v *qdrant.Value) any {
	if v == nil {
		return nil
	}
	switch val := v.Kind.(type) {
	case *qdrant.Value_StringValue:
		return val.StringValue
	case *qdrant.Value_IntegerValue:
		return val.IntegerValue
	case *qdrant.Value_DoubleValue:
		return val.DoubleValue
	case *qdrant.Value_BoolValue:
		return val.BoolValue
	default:
		return nil
	}
}
