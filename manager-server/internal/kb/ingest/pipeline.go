package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino-ext/components/document/loader/file"
	urlLoader "github.com/cloudwego/eino-ext/components/document/loader/url"
	"github.com/cloudwego/eino-ext/components/document/parser/html"
	"github.com/cloudwego/eino-ext/components/document/parser/pdf"
	"github.com/cloudwego/eino-ext/components/document/parser/xlsx"
	"github.com/cloudwego/eino-ext/components/document/transformer/splitter/markdown"
	"github.com/cloudwego/eino-ext/components/document/transformer/splitter/recursive"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

const (
	fieldSource = "_source"
	fieldExt    = "_extension"

	titleH1 = "h1"
	titleH2 = "h2"
	titleH3 = "h3"

	defaultChunkSize    = 1000
	defaultChunkOverlap = 100
)

// Config holds runtime options for chunk generation.
type Config struct {
	ChunkSize    int
	ChunkOverlap int
}

// DocumentPipeline abstracts document preprocessing.
type DocumentPipeline interface {
	Process(ctx context.Context, uri string, cfg Config) ([]*schema.Document, error)
}

// Pipeline processes documents into schema documents aligned with go-rag behaviour.
type Pipeline struct {
	loader document.Loader
}

var _ DocumentPipeline = (*Pipeline)(nil)

// NewPipeline constructs a new Pipeline instance.
func NewPipeline(ctx context.Context) (DocumentPipeline, error) {
	ldr, err := newLoader(ctx)
	if err != nil {
		return nil, err
	}
	return &Pipeline{loader: ldr}, nil
}

// Process loads, parses, and chunks the supplied URI.
func (p *Pipeline) Process(ctx context.Context, uri string, cfg Config) ([]*schema.Document, error) {
	if p == nil || p.loader == nil {
		return nil, errors.New("ingest pipeline not initialised")
	}
	if strings.TrimSpace(uri) == "" {
		return nil, errors.New("document uri required")
	}

	source := document.Source{URI: uri}
	rawDocs, err := p.loader.Load(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("load source: %w", err)
	}
	if len(rawDocs) == 0 {
		return nil, errors.New("loader produced no documents")
	}

	tr, err := newDocumentTransformer(ctx, cfg)
	if err != nil {
		return nil, err
	}
	segmented, err := tr.Transform(ctx, rawDocs)
	if err != nil {
		return nil, fmt.Errorf("transform documents: %w", err)
	}
	if len(segmented) == 0 {
		return nil, errors.New("transformer produced no segments")
	}

	merged, err := docAddIDAndMerge(ctx, segmented)
	if err != nil {
		return nil, err
	}
	return merged, nil
}

func newLoader(ctx context.Context) (document.Loader, error) {
	parser, err := newParser(ctx)
	if err != nil {
		return nil, err
	}
	fileLoader, err := file.NewFileLoader(ctx, &file.FileLoaderConfig{
		UseNameAsID: false,
		Parser:      parser,
	})
	if err != nil {
		return nil, err
	}
	remoteLoader, err := urlLoader.NewLoader(ctx, &urlLoader.LoaderConfig{})
	if err != nil {
		return nil, err
	}
	return &multiLoader{
		fileLoader: fileLoader,
		urlLoader:  remoteLoader,
	}, nil
}

func newParser(ctx context.Context) (parser.Parser, error) {
	textParser := parser.TextParser{}

	htmlParser, err := html.NewParser(ctx, &html.Config{
		Selector: ptr("body"),
	})
	if err != nil {
		return nil, err
	}
	xlsxParser, err := xlsx.NewXlsxParser(ctx, nil)
	if err != nil {
		return nil, err
	}
	pdfParser, err := pdf.NewPDFParser(ctx, &pdf.Config{})
	if err != nil {
		return nil, err
	}

	return parser.NewExtParser(ctx, &parser.ExtParserConfig{
		Parsers: map[string]parser.Parser{
			".html": htmlParser,
			".htm":  htmlParser,
			".pdf":  pdfParser,
			".xlsx": xlsxParser,
			".csv":  textParser,
			".md":   textParser,
			".txt":  textParser,
		},
		FallbackParser: textParser,
	})
}

func newDocumentTransformer(ctx context.Context, cfg Config) (document.Transformer, error) {
	chunkSize := cfg.ChunkSize
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}
	chunkOverlap := cfg.ChunkOverlap
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}

	recCfg := &recursive.Config{
		ChunkSize:   chunkSize,
		OverlapSize: chunkOverlap,
		Separators:  []string{"\n", "。", "?", "？", "!", "！"},
	}
	recTrans, err := recursive.NewSplitter(ctx, recCfg)
	if err != nil {
		return nil, err
	}
	mdTrans, err := markdown.NewHeaderSplitter(ctx, &markdown.HeaderConfig{
		Headers: map[string]string{
			"#":   titleH1,
			"##":  titleH2,
			"###": titleH3,
		},
		TrimHeaders: false,
	})
	if err != nil {
		return nil, err
	}
	return &transformer{
		markdown:  mdTrans,
		recursive: recTrans,
	}, nil
}

type transformer struct {
	markdown  document.Transformer
	recursive document.Transformer
}

func (t *transformer) Transform(ctx context.Context, docs []*schema.Document, opts ...document.TransformerOption) ([]*schema.Document, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	isMarkdown := false
	for _, doc := range docs {
		if doc == nil || doc.MetaData == nil {
			continue
		}
		if ext, ok := doc.MetaData[fieldExt].(string); ok && strings.EqualFold(ext, ".md") {
			isMarkdown = true
			break
		}
	}
	if isMarkdown && t.markdown != nil {
		return t.markdown.Transform(ctx, docs, opts...)
	}
	return t.recursive.Transform(ctx, docs, opts...)
}

func docAddIDAndMerge(_ context.Context, docs []*schema.Document) ([]*schema.Document, error) {
	if len(docs) == 0 {
		return docs, nil
	}
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		doc.ID = uuid.NewString()
	}
	ext := extensionOf(docs[0])
	switch ext {
	case ".md":
		return mergeMarkdown(docs), nil
	case ".xlsx":
		return mergeXlsx(docs)
	default:
		return docs, nil
	}
}

func mergeMarkdown(docs []*schema.Document) []*schema.Document {
	result := make([]*schema.Document, 0, len(docs))
	var current *schema.Document
	var lastSource string
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		source := metaString(doc, fieldSource)
		if current == nil {
			current = cloneDoc(doc)
			lastSource = source
			continue
		}

		if source != lastSource ||
			len(current.Content)+len(doc.Content) > 512 ||
			metaString(current, titleH1) != metaString(doc, titleH1) ||
			(metaString(current, titleH2) != "" && metaString(current, titleH2) != metaString(doc, titleH2)) {
			result = append(result, finalizeMarkdown(current))
			current = cloneDoc(doc)
			lastSource = source
			continue
		}

		mergeTitle(current, doc, titleH2)
		mergeTitle(current, doc, titleH3)
		if current.Content != "" {
			current.Content += "\n"
		}
		current.Content += doc.Content
	}
	if current != nil {
		result = append(result, finalizeMarkdown(current))
	}
	return result
}

func finalizeMarkdown(doc *schema.Document) *schema.Document {
	if doc.MetaData == nil {
		return doc
	}
	var titles []string
	for _, key := range []string{titleH1, titleH2, titleH3} {
		if value, ok := doc.MetaData[key].(string); ok && strings.TrimSpace(value) != "" {
			titles = append(titles, fmt.Sprintf("%s:%s", key, value))
		}
	}
	if len(titles) > 0 {
		doc.Content = strings.Join(titles, " ") + "\n" + doc.Content
	}
	return doc
}

func mergeXlsx(docs []*schema.Document) ([]*schema.Document, error) {
	for _, doc := range docs {
		if doc == nil || doc.MetaData == nil {
			continue
		}
		if row, ok := doc.MetaData["_row"]; ok {
			data, err := json.Marshal(row)
			if err != nil {
				return nil, fmt.Errorf("marshal xlsx row: %w", err)
			}
			doc.Content = string(data)
		}
	}
	return docs, nil
}

func cloneDoc(doc *schema.Document) *schema.Document {
	if doc == nil {
		return nil
	}
	newDoc := &schema.Document{
		ID:       doc.ID,
		Content:  doc.Content,
		MetaData: map[string]any{},
	}
	for key, value := range doc.MetaData {
		newDoc.MetaData[key] = value
	}
	return newDoc
}

func mergeTitle(dst, src *schema.Document, key string) {
	if dst == nil || src == nil {
		return
	}
	dstVal := metaString(dst, key)
	srcVal := metaString(src, key)
	switch {
	case dstVal == "" && srcVal != "":
		dst.MetaData[key] = srcVal
	case dstVal != "" && srcVal != "" && dstVal != srcVal:
		dst.MetaData[key] = strings.Join([]string{dstVal, srcVal}, ",")
	}
}

func extensionOf(doc *schema.Document) string {
	if doc == nil || doc.MetaData == nil {
		return ""
	}
	if v, ok := doc.MetaData[fieldExt].(string); ok && v != "" {
		return strings.ToLower(v)
	}
	if v, ok := doc.MetaData[fieldSource].(string); ok && v != "" {
		return strings.ToLower(filepath.Ext(v))
	}
	return ""
}

func metaString(doc *schema.Document, key string) string {
	if doc == nil || doc.MetaData == nil {
		return ""
	}
	if value, ok := doc.MetaData[key]; ok {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
}

type multiLoader struct {
	fileLoader document.Loader
	urlLoader  document.Loader
}

func (m *multiLoader) Load(ctx context.Context, src document.Source, opts ...document.LoaderOption) ([]*schema.Document, error) {
	if isURL(src.URI) {
		return m.urlLoader.Load(ctx, src, opts...)
	}
	return m.fileLoader.Load(ctx, src, opts...)
}

func isURL(str string) bool {
	u, err := url.Parse(str)
	if err != nil {
		return false
	}
	return u.Scheme != "" && u.Host != ""
}

func ptr[T any](v T) *T {
	return &v
}
