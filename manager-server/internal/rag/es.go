package rag

import (
	"context"
	"fmt"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/indices/create"
	"github.com/elastic/go-elasticsearch/v8/typedapi/indices/exists"
	"github.com/elastic/go-elasticsearch/v8/typedapi/indices/get"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
)

func ensureIndex(ctx context.Context, client *elasticsearch.Client, indexName string, dims int) error {
	if indexName == "" {
		return fmt.Errorf("index name required")
	}
	if dims <= 0 {
		dims = DefaultEmbeddingDims
	}
	existsFn := exists.NewExistsFunc(client)
	ok, err := existsFn(indexName).Do(ctx)
	if err != nil {
		return fmt.Errorf("check index: %w", err)
	}
	if ok {
		getFn := get.NewGetFunc(client)
		resp, err := getFn(indexName).Do(ctx)
		if err != nil {
			return fmt.Errorf("get index mapping: %w", err)
		}
		if mapping, ok := resp[indexName]; ok {
			if prop, ok := mapping.Mappings.Properties[FieldContentVector]; ok {
				if vecProp, ok := prop.(*types.DenseVectorProperty); ok && vecProp.Dims != nil {
					existing := int(*vecProp.Dims)
					if existing != dims {
						return fmt.Errorf("index %s already exists with vector dims %d (expected %d)", indexName, existing, dims)
					}
				}
			}
		}
		return nil
	}
	mapping := &types.TypeMapping{
		Properties: map[string]types.Property{
			FieldContent:       types.NewTextProperty(),
			FieldExtra:         types.NewTextProperty(),
			FieldKnowledgeName: types.NewKeywordProperty(),
			FieldContentVector: &types.DenseVectorProperty{
				Dims:       ptr(dims),
				Index:      ptr(true),
				Similarity: ptr("cosine"),
			},
		},
	}
	req := &create.Request{Mappings: mapping}
	createFn := create.NewCreateFunc(client)
	if _, err := createFn(indexName).Request(req).Do(ctx); err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	return nil
}

func deleteDocument(ctx context.Context, client *elasticsearch.Client, indexName, docID string) error {
	if docID == "" {
		return fmt.Errorf("document id required")
	}
	resp, err := client.Delete(indexName, docID)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	defer resp.Body.Close()
	if resp.IsError() {
		return fmt.Errorf("delete document: %s", resp.String())
	}
	return nil
}
