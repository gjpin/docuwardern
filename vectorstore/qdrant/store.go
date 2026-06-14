package qdrantstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	qdrant "github.com/qdrant/go-client/qdrant"
	"github.com/zero/docuwarden/vectorstore"
)

type Config struct {
	Host   string
	Port   int
	APIKey string
	UseTLS bool
}

type Store struct{ client *qdrant.Client }

const (
	denseVectorName    = "dense"
	sparseVectorName   = "sparse"
	indexSchemaVersion = 3
)

func (s *Store) LoadCachedDenseVectors(ctx context.Context, source, version, embeddingProfile string, inputHashes []string) (map[string][]float32, error) {
	result := map[string][]float32{}
	if source == "" || embeddingProfile == "" || len(inputHashes) == 0 {
		return result, nil
	}
	alias := aliasName(source, version)
	aliases, err := s.client.ListAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Qdrant aliases: %w", err)
	}
	collection := ""
	for _, item := range aliases {
		if item.GetAliasName() == alias {
			collection = item.GetCollectionName()
			break
		}
	}
	if collection == "" {
		return result, nil
	}
	info, err := s.client.GetCollectionInfo(ctx, collection)
	if err != nil {
		return nil, fmt.Errorf("inspect cached Qdrant collection: %w", err)
	}
	metadata := info.GetConfig().GetMetadata()
	if integerValue(metadata, "docuwarden_schema") != indexSchemaVersion || stringValue(metadata, "embedding_profile") != embeddingProfile {
		return result, nil
	}
	wanted := make(map[string]bool, len(inputHashes))
	for _, hash := range inputHashes {
		wanted[hash] = true
	}
	iterator := s.client.ScrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: collection, Limit: qdrant.PtrOf(uint32(256)),
		WithPayload: qdrant.NewWithPayloadInclude("input_hash"),
		WithVectors: qdrant.NewWithVectorsInclude(denseVectorName),
	})
	for {
		points, err := iterator.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("load cached embeddings from Qdrant: %w", err)
		}
		for _, point := range points {
			hash := stringValue(point.GetPayload(), "input_hash")
			if !wanted[hash] || result[hash] != nil {
				continue
			}
			named := point.GetVectors().GetVectors()
			if named == nil || named.GetVectors()[denseVectorName] == nil {
				continue
			}
			vector := denseVectorData(named.GetVectors()[denseVectorName])
			if len(vector) > 0 {
				result[hash] = append([]float32(nil), vector...)
			}
		}
	}
	return result, nil
}

func denseVectorData(vector *qdrant.VectorOutput) []float32 {
	if dense := vector.GetDense(); dense != nil {
		return dense.GetData()
	}
	return vector.GetData()
}

func New(cfg Config) (*Store, error) {
	client, err := qdrant.NewClient(&qdrant.Config{Host: cfg.Host, Port: cfg.Port, APIKey: cfg.APIKey, UseTLS: cfg.UseTLS})
	if err != nil {
		return nil, fmt.Errorf("connect to Qdrant: %w", err)
	}
	return &Store{client: client}, nil
}

func (s *Store) Close() error { return s.client.Close() }

func (s *Store) ReplaceSnapshot(ctx context.Context, snapshot vectorstore.Snapshot) (err error) {
	if snapshot.Source == "" || len(snapshot.Points) == 0 {
		return errors.New("snapshot source and points are required")
	}
	if snapshot.EmbeddingProfile == "" {
		return errors.New("snapshot embedding profile is required")
	}
	dimension := len(snapshot.Points[0].DenseVector)
	if dimension == 0 {
		return errors.New("snapshot vectors are empty")
	}
	for i, point := range snapshot.Points {
		if point.Source != snapshot.Source || point.Version != snapshot.Version {
			return fmt.Errorf("point %d metadata does not match snapshot", i)
		}
		if point.InputHash == "" {
			return fmt.Errorf("point %d embedding input hash is required", i)
		}
		if len(point.DenseVector) != dimension {
			return fmt.Errorf("point %d vector dimension mismatch", i)
		}
		if len(point.Sparse.Indices) == 0 || len(point.Sparse.Indices) != len(point.Sparse.Values) {
			return fmt.Errorf("point %d sparse vector is invalid", i)
		}
	}
	physical := physicalName(snapshot.Source, snapshot.Version)
	metadata := map[string]any{
		"docuwarden_schema": indexSchemaVersion, "source": snapshot.Source, "version": snapshot.Version,
		"display_name": snapshot.DisplayName, "description": snapshot.Description, "tags": anyStrings(snapshot.Tags),
		"seed_url": snapshot.SeedURL, "document_count": snapshot.DocumentCount, "chunk_count": len(snapshot.Points),
		"complete": snapshot.Complete, "indexed_at": snapshot.IndexedAt.UTC().Format(time.RFC3339Nano),
		"embedding_model":   snapshot.EmbeddingModel,
		"embedding_profile": snapshot.EmbeddingProfile,
	}
	if err := s.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: physical,
		VectorsConfig: qdrant.NewVectorsConfigMap(map[string]*qdrant.VectorParams{
			denseVectorName: {Size: uint64(dimension), Distance: qdrant.Distance_Cosine},
		}),
		SparseVectorsConfig: qdrant.NewSparseVectorsConfig(map[string]*qdrant.SparseVectorParams{
			sparseVectorName: {Modifier: qdrant.Modifier_Idf.Enum()},
		}),
		Metadata: qdrant.NewValueMap(metadata),
	}); err != nil {
		return fmt.Errorf("create Qdrant collection: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = s.client.DeleteCollection(context.WithoutCancel(ctx), physical)
		}
	}()
	points := make([]*qdrant.PointStruct, len(snapshot.Points))
	for i, point := range snapshot.Points {
		headings := make([]any, len(point.HeadingPath))
		for j, value := range point.HeadingPath {
			headings[j] = value
		}
		payload, payloadErr := qdrant.TryValueMap(map[string]any{
			"source": point.Source, "version": point.Version, "url": point.URL, "title": point.Title,
			"heading_path": headings, "chunk_index": point.ChunkIndex, "markdown": point.Markdown,
			"content_hash": point.ContentHash, "crawled_at": point.CrawledAt.UTC().Format(time.RFC3339Nano),
			"input_hash":       point.InputHash,
			"allow_incomplete": snapshot.AllowIncomplete,
			"index_schema":     indexSchemaVersion,
		})
		if payloadErr != nil {
			return fmt.Errorf("encode point payload: %w", payloadErr)
		}
		points[i] = &qdrant.PointStruct{Id: qdrant.NewID(uuidFromHash(point.ID)), Vectors: qdrant.NewVectorsMap(map[string]*qdrant.Vector{
			denseVectorName:  qdrant.NewVectorDense(point.DenseVector),
			sparseVectorName: qdrant.NewVectorSparse(point.Sparse.Indices, point.Sparse.Values),
		}), Payload: payload}
	}
	wait := true
	for start := 0; start < len(points); start += 128 {
		end := min(start+128, len(points))
		if _, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{CollectionName: physical, Wait: &wait, Points: points[start:end]}); err != nil {
			return fmt.Errorf("upload Qdrant points: %w", err)
		}
	}
	count, err := s.client.Count(ctx, &qdrant.CountPoints{CollectionName: physical, Exact: qdrant.PtrOf(true)})
	if err != nil {
		return fmt.Errorf("validate Qdrant collection: %w", err)
	}
	if count != uint64(len(points)) {
		return fmt.Errorf("Qdrant point count mismatch: uploaded %d, found %d", len(points), count)
	}
	aliases, err := s.client.ListAliases(ctx)
	if err != nil {
		return fmt.Errorf("list Qdrant aliases: %w", err)
	}
	versionAlias := aliasName(snapshot.Source, snapshot.Version)
	defaultAlias := aliasName(snapshot.Source, "")
	existing := map[string]bool{}
	for _, alias := range aliases {
		existing[alias.GetAliasName()] = true
	}
	var actions []*qdrant.AliasOperations
	aliasNames := []string{defaultAlias}
	if versionAlias != defaultAlias {
		aliasNames = append([]string{versionAlias}, aliasNames...)
	}
	for _, alias := range aliasNames {
		if existing[alias] {
			actions = append(actions, qdrant.NewAliasDelete(alias))
		}
		actions = append(actions, qdrant.NewAliasCreate(alias, physical))
	}
	if err := s.client.UpdateAliases(ctx, actions); err != nil {
		return fmt.Errorf("publish Qdrant aliases: %w", err)
	}
	published = true
	retention := snapshot.Retention
	if retention <= 0 {
		retention = 2
	}
	// Publication is already atomic and successful; stale-snapshot cleanup is best effort.
	_ = s.cleanup(ctx, snapshot.Source, retention)
	return nil
}

func (s *Store) ListSources(ctx context.Context) (vectorstore.Catalog, error) {
	aliases, err := s.client.ListAliases(ctx)
	if err != nil {
		return vectorstore.Catalog{}, fmt.Errorf("list Qdrant aliases: %w", err)
	}
	byCollection := map[string][]string{}
	for _, alias := range aliases {
		byCollection[alias.GetCollectionName()] = append(byCollection[alias.GetCollectionName()], alias.GetAliasName())
	}
	bySource := map[string]*vectorstore.CatalogSource{}
	collections := make([]string, 0, len(byCollection))
	for collection := range byCollection {
		collections = append(collections, collection)
	}
	sort.Strings(collections)
	for _, collection := range collections {
		names := byCollection[collection]
		info, err := s.client.GetCollectionInfo(ctx, collection)
		if err != nil {
			return vectorstore.Catalog{}, fmt.Errorf("inspect Qdrant collection %s: %w", collection, err)
		}
		metadata := info.GetConfig().GetMetadata()
		source := stringValue(metadata, "source")
		if source == "" || integerValue(metadata, "docuwarden_schema") == 0 {
			continue
		}
		version := stringValue(metadata, "version")
		isDefault := false
		for _, name := range names {
			if name == aliasName(source, "") {
				isDefault = true
			}
		}
		entry := bySource[source]
		if entry == nil {
			entry = &vectorstore.CatalogSource{Source: source, DisplayName: stringValue(metadata, "display_name"), Description: stringValue(metadata, "description"), Tags: listValue(metadata, "tags")}
			bySource[source] = entry
		}
		if isDefault {
			entry.DefaultVersion = version
			entry.DisplayName = stringValue(metadata, "display_name")
			entry.Description = stringValue(metadata, "description")
			entry.Tags = listValue(metadata, "tags")
		}
		entry.Versions = append(entry.Versions, vectorstore.CatalogVersion{
			Version: version, SeedURL: stringValue(metadata, "seed_url"), DocumentCount: int(integerValue(metadata, "document_count")),
			ChunkCount: catalogChunkCount(metadata, info.GetPointsCount()), IndexedAt: stringValue(metadata, "indexed_at"),
			Complete: boolValue(metadata, "complete"), EmbeddingModel: stringValue(metadata, "embedding_model"),
		})
	}
	catalog := vectorstore.Catalog{SchemaVersion: 1}
	for _, source := range bySource {
		sort.Slice(source.Versions, func(i, j int) bool { return source.Versions[i].Version < source.Versions[j].Version })
		catalog.Sources = append(catalog.Sources, *source)
	}
	sort.Slice(catalog.Sources, func(i, j int) bool { return catalog.Sources[i].Source < catalog.Sources[j].Source })
	return catalog, nil
}

func (s *Store) ListDocuments(ctx context.Context, source, version string) (vectorstore.DocumentCatalog, error) {
	if source == "" {
		return vectorstore.DocumentCatalog{}, errors.New("source is required")
	}
	collection := aliasName(source, version)
	iterator := s.client.ScrollAll(ctx, &qdrant.ScrollPoints{
		CollectionName: collection, Limit: qdrant.PtrOf(uint32(256)),
		WithPayload: qdrant.NewWithPayloadInclude("source", "version", "url", "title", "crawled_at"),
		WithVectors: qdrant.NewWithVectors(false),
	})
	documents := map[string]vectorstore.CatalogDocument{}
	resolvedVersion := version
	for {
		points, err := iterator.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return vectorstore.DocumentCatalog{}, fmt.Errorf("list documents from Qdrant: %w", err)
		}
		for _, point := range points {
			payload := point.GetPayload()
			if resolvedVersion == "" {
				resolvedVersion = stringValue(payload, "version")
			}
			url := stringValue(payload, "url")
			if url != "" {
				documents[url] = vectorstore.CatalogDocument{URL: url, Title: stringValue(payload, "title"), CrawledAt: stringValue(payload, "crawled_at")}
			}
		}
	}
	result := vectorstore.DocumentCatalog{SchemaVersion: 1, Source: source, Version: resolvedVersion}
	for _, document := range documents {
		result.Documents = append(result.Documents, document)
	}
	sort.Slice(result.Documents, func(i, j int) bool { return result.Documents[i].URL < result.Documents[j].URL })
	return result, nil
}

func catalogChunkCount(metadata map[string]*qdrant.Value, fallback uint64) int {
	if count := integerValue(metadata, "chunk_count"); count > 0 {
		return int(count)
	}
	return int(fallback)
}

func anyStrings(values []string) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

func (s *Store) Search(ctx context.Context, request vectorstore.SearchRequest) ([]vectorstore.Candidate, error) {
	if request.Source == "" || len(request.Dense) == 0 {
		return nil, errors.New("source and query vector are required")
	}
	if request.Limit <= 0 {
		request.Limit = 40
	}
	if request.Mode == "" {
		request.Mode = vectorstore.SearchModeHybrid
	}
	collection := aliasName(request.Source, request.Version)
	if request.Mode == vectorstore.SearchModeDense {
		points, err := s.queryDense(ctx, collection, request.Dense, request.Limit)
		if err != nil {
			return nil, fmt.Errorf("query Qdrant dense index; reindex legacy collections: %w", err)
		}
		return candidates(points, nil, nil), nil
	}
	if len(request.Sparse.Indices) == 0 {
		return nil, errors.New("hybrid search requires a sparse query vector")
	}
	dense, err := s.queryDense(ctx, collection, request.Dense, request.Limit)
	if err != nil {
		return nil, fmt.Errorf("query Qdrant dense vector; reindex the source with the current Docuwarden version: %w", err)
	}
	sparsePoints, err := s.querySparse(ctx, collection, request.Sparse, request.Limit)
	if err != nil {
		return nil, fmt.Errorf("query Qdrant sparse vector; reindex the source with the current Docuwarden version: %w", err)
	}
	fused, err := s.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: collection,
		Prefetch: []*qdrant.PrefetchQuery{
			{Query: qdrant.NewQueryDense(request.Dense), Using: qdrant.PtrOf(denseVectorName), Limit: qdrant.PtrOf(uint64(request.Limit))},
			{Query: qdrant.NewQuerySparse(request.Sparse.Indices, request.Sparse.Values), Using: qdrant.PtrOf(sparseVectorName), Limit: qdrant.PtrOf(uint64(request.Limit))},
		},
		Query: qdrant.NewQueryRRF(&qdrant.Rrf{}), Limit: qdrant.PtrOf(uint64(request.Limit)), WithPayload: qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("query Qdrant hybrid index: %w", err)
	}
	return candidates(fused, dense, sparsePoints), nil
}

func (s *Store) queryDense(ctx context.Context, collection string, vector []float32, limit int) ([]*qdrant.ScoredPoint, error) {
	return s.client.Query(ctx, &qdrant.QueryPoints{CollectionName: collection, Query: qdrant.NewQueryDense(vector), Using: qdrant.PtrOf(denseVectorName), Limit: qdrant.PtrOf(uint64(limit)), WithPayload: qdrant.NewWithPayload(true)})
}

func (s *Store) querySparse(ctx context.Context, collection string, vector vectorstore.SparseVector, limit int) ([]*qdrant.ScoredPoint, error) {
	return s.client.Query(ctx, &qdrant.QueryPoints{CollectionName: collection, Query: qdrant.NewQuerySparse(vector.Indices, vector.Values), Using: qdrant.PtrOf(sparseVectorName), Limit: qdrant.PtrOf(uint64(limit)), WithPayload: qdrant.NewWithPayload(true)})
}

func candidates(primary, dense, sparsePoints []*qdrant.ScoredPoint) []vectorstore.Candidate {
	denseScores := scoreMap(dense)
	sparseScores := scoreMap(sparsePoints)
	results := make([]vectorstore.Candidate, 0, len(primary))
	for _, item := range primary {
		payload := item.GetPayload()
		id := pointIDString(item.GetId())
		candidate := vectorstore.Candidate{Point: vectorstore.Point{ID: id,
			Source: stringValue(payload, "source"), Version: stringValue(payload, "version"), URL: stringValue(payload, "url"), Title: stringValue(payload, "title"),
			HeadingPath: listValue(payload, "heading_path"), ChunkIndex: int(integerValue(payload, "chunk_index")), Markdown: stringValue(payload, "markdown"),
			ContentHash: stringValue(payload, "content_hash"), CrawledAt: timeValue(payload, "crawled_at"),
		}, DenseScore: denseScores[id], SparseScore: sparseScores[id], FusionScore: float64(item.GetScore())}
		if len(dense) == 0 {
			candidate.DenseScore = float64(item.GetScore())
			candidate.FusionScore = 0
		}
		results = append(results, candidate)
	}
	return results
}

func scoreMap(points []*qdrant.ScoredPoint) map[string]float64 {
	result := map[string]float64{}
	for _, point := range points {
		result[pointIDString(point.GetId())] = float64(point.GetScore())
	}
	return result
}

func pointIDString(id *qdrant.PointId) string {
	if id == nil {
		return ""
	}
	if value := id.GetUuid(); value != "" {
		return value
	}
	return fmt.Sprintf("%d", id.GetNum())
}

func (s *Store) cleanup(ctx context.Context, source string, retention int) error {
	collections, err := s.client.ListCollections(ctx)
	if err != nil {
		return err
	}
	aliases, err := s.client.ListAliases(ctx)
	if err != nil {
		return err
	}
	active := map[string]bool{}
	for _, alias := range aliases {
		active[alias.GetCollectionName()] = true
	}
	prefix := collectionPrefix(source)
	var owned []string
	for _, name := range collections {
		if strings.HasPrefix(name, prefix) {
			owned = append(owned, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(owned)))
	kept := 0
	for _, name := range owned {
		if active[name] || kept < retention {
			kept++
			continue
		}
		if err := s.client.DeleteCollection(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

func aliasName(source, version string) string {
	label := "default"
	if version != "" {
		label = safe(version)
	}
	return "dw_" + safe(source) + "_" + label + "_" + shortHash(source+"\x00"+version)
}

func collectionPrefix(source string) string {
	return safe(source) + "__"
}
func physicalName(source, version string) string {
	random := make([]byte, 4)
	_, _ = rand.Read(random)
	versionLabel := "unversioned"
	if version != "" {
		versionLabel = safe(version)
	}
	return fmt.Sprintf("%s%s__snapshot_%d_%s", collectionPrefix(source), versionLabel, time.Now().UTC().UnixNano(), hex.EncodeToString(random))
}
func safe(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			out.WriteRune(r)
		} else {
			out.WriteByte('_')
		}
	}
	result := strings.Trim(out.String(), "_")
	if result == "" {
		return "source"
	}
	if len(result) > 24 {
		result = result[:24]
	}
	return result
}
func shortHash(value string) string { hash := corpusHash(value); return hash[:10] }
func corpusHash(value string) string {
	// Kept local so vector storage does not depend on artifact serialization.
	bytes := []byte(value)
	var h uint64 = 14695981039346656037
	for _, b := range bytes {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return fmt.Sprintf("%016x", h)
}
func uuidFromHash(hash string) string {
	clean := strings.ReplaceAll(hash, "-", "")
	for len(clean) < 32 {
		clean += corpusHash(clean)
	}
	clean = clean[:32]
	return clean[:8] + "-" + clean[8:12] + "-" + clean[12:16] + "-" + clean[16:20] + "-" + clean[20:]
}
func stringValue(payload map[string]*qdrant.Value, key string) string {
	if payload[key] == nil {
		return ""
	}
	return payload[key].GetStringValue()
}
func integerValue(payload map[string]*qdrant.Value, key string) int64 {
	if payload[key] == nil {
		return 0
	}
	return payload[key].GetIntegerValue()
}
func boolValue(payload map[string]*qdrant.Value, key string) bool {
	if payload[key] == nil {
		return false
	}
	return payload[key].GetBoolValue()
}
func listValue(payload map[string]*qdrant.Value, key string) []string {
	if payload[key] == nil || payload[key].GetListValue() == nil {
		return nil
	}
	values := payload[key].GetListValue().GetValues()
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.GetStringValue())
	}
	return out
}
func timeValue(payload map[string]*qdrant.Value, key string) time.Time {
	value, _ := time.Parse(time.RFC3339Nano, stringValue(payload, key))
	return value
}
