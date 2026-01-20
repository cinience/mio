package functions

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	"backend-server/internal/domain/tools/utils"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

const (
	GetNewsFromChinaNewsFunctionName = "get_news_from_chinanews"
)

// GetNewsFromChinaNewsFunctionDesc defines the function description for LLM
var GetNewsFromChinaNewsFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        GetNewsFromChinaNewsFunctionName,
		"description": "从中国新闻网获取最新新闻资讯，包括国内外重要新闻",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"count": map[string]interface{}{
					"type":        "integer",
					"description": "要获取的新闻条数，默认为5条，最多10条",
					"default":     5,
					"minimum":     1,
					"maximum":     10,
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "新闻分类，可选值：all(全部)、domestic(国内)、international(国际)、finance(财经)、tech(科技)等",
					"default":     "all",
				},
			},
			"required": []string{},
		},
	},
}

// RSS feed structure for China News
type ChinaNewsRSS struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		Items       []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
			GUID        string `xml:"guid"`
		} `xml:"item"`
	} `xml:"channel"`
}

// NewsItem represents a single news item
type NewsItem struct {
	Title       string    `json:"title"`
	Link        string    `json:"link"`
	Description string    `json:"description"`
	PubDate     time.Time `json:"pub_date"`
	Source      string    `json:"source"`
}

// GetNewsFromChinaNewsFunction implements the China News function
type GetNewsFromChinaNewsFunction struct{}

// NewGetNewsFromChinaNewsFunction creates a new China News function
func NewGetNewsFromChinaNewsFunction(args map[string]interface{}) *GetNewsFromChinaNewsFunction {
	return &GetNewsFromChinaNewsFunction{}
}

// Execute implements the function execution
func (f *GetNewsFromChinaNewsFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}

	// Extract parameters
	count := 5
	if c, ok := args["count"].(float64); ok {
		count = int(c)
	} else if c, ok := args["count"].(int); ok {
		count = c
	}

	// Limit count to reasonable range
	if count < 1 {
		count = 1
	} else if count > 10 {
		count = 10
	}

	category, _ := args["category"].(string)
	if category == "" {
		category = "all"
	}

	if logger != nil {
		logger.Info("获取中国新闻网新闻", "count", count, "category", category)
	}

	// Try to get news from cache first
	cacheKey := fmt.Sprintf("chinanews_%s_%d", category, count)
	if cachedData, found := utils.GetCache(utils.CacheTypeNews, cacheKey); found {
		if logger != nil {
			logger.Debug("从缓存获取中国新闻网数据")
		}
		return types.NewActionResponse(types.ActionReqLLM, cachedData, nil), nil
	}

	// Get RSS feed URL based on category
	rssURL := f.getRSSURL(category)

	// Fetch RSS feed
	newsItems, err := f.fetchRSSFeed(ctx, rssURL)
	if err != nil {
		if logger != nil {
			logger.Error("获取中国新闻网RSS失败", "error", err)
		}
		return types.NewActionResponse(types.ActionReqLLM, "抱歉，获取新闻失败，请稍后再试", nil), nil
	}

	// Limit to requested count
	if len(newsItems) > count {
		newsItems = newsItems[:count]
	}

	// Format response
	response := f.formatNewsResponse(newsItems, category)

	// Cache the response
	utils.SetCacheWithDefaultTTL(utils.CacheTypeNews, cacheKey, response)

	// Store last news link in connection for potential follow-up
	if conn != nil && len(newsItems) > 0 {
		lastNewsLink := map[string]interface{}{
			"source": "chinanews",
			"links":  f.extractLinks(newsItems),
			"time":   time.Now().Unix(),
		}
		conn.SetLastNewsLink(lastNewsLink)
	}

	if logger != nil {
		logger.Info("中国新闻网新闻获取成功", "count", len(newsItems))
	}

	return types.NewActionResponse(types.ActionReqLLM, response, nil), nil
}

// getRSSURL returns the RSS URL for the specified category
func (f *GetNewsFromChinaNewsFunction) getRSSURL(category string) string {
	baseURL := "https://www.chinanews.com.cn"

	switch category {
	case "domestic", "国内":
		return baseURL + "/rss/scroll-news.xml"
	case "international", "国际":
		return baseURL + "/rss/world.xml"
	case "finance", "财经":
		return baseURL + "/rss/finance.xml"
	case "tech", "科技":
		return baseURL + "/rss/it.xml"
	case "sports", "体育":
		return baseURL + "/rss/sports.xml"
	case "entertainment", "娱乐":
		return baseURL + "/rss/ent.xml"
	default:
		// Default to general news
		return baseURL + "/rss/scroll-news.xml"
	}
}

// fetchRSSFeed fetches and parses the RSS feed
func (f *GetNewsFromChinaNewsFunction) fetchRSSFeed(ctx context.Context, rssURL string) ([]NewsItem, error) {
	// Set headers to mimic a real browser
	headers := map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
		"Accept":          "application/rss+xml, application/xml, text/xml",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"Cache-Control":   "no-cache",
	}

	// Fetch RSS content
	body, err := utils.Get(ctx, rssURL, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch RSS feed: %v", err)
	}

	// Parse RSS XML
	var rss ChinaNewsRSS
	if err := xml.Unmarshal(body, &rss); err != nil {
		return nil, fmt.Errorf("failed to parse RSS XML: %v", err)
	}

	// Convert to NewsItem slice
	var newsItems []NewsItem
	for _, item := range rss.Channel.Items {
		// Parse publication date
		pubDate, err := f.parseChineseDate(item.PubDate)
		if err != nil {
			// If date parsing fails, use current time
			pubDate = time.Now()
		}

		// Clean up description
		description := f.cleanDescription(item.Description)

		newsItem := NewsItem{
			Title:       strings.TrimSpace(item.Title),
			Link:        strings.TrimSpace(item.Link),
			Description: description,
			PubDate:     pubDate,
			Source:      "中国新闻网",
		}

		newsItems = append(newsItems, newsItem)
	}

	return newsItems, nil
}

// parseChineseDate parses Chinese date format
func (f *GetNewsFromChinaNewsFunction) parseChineseDate(dateStr string) (time.Time, error) {
	// Try common date formats
	formats := []string{
		"Mon, 02 Jan 2006 15:04:05 MST",
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"2006-01-02 15:04:05",
		"2006/01/02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05+08:00",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

// cleanDescription removes HTML tags and cleans up the description
func (f *GetNewsFromChinaNewsFunction) cleanDescription(description string) string {
	// Remove HTML tags
	re := regexp.MustCompile(`<[^>]*>`)
	cleaned := re.ReplaceAllString(description, "")

	// Remove extra whitespace
	cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " ")

	// Trim and limit length
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) > 200 {
		cleaned = cleaned[:200] + "..."
	}

	return cleaned
}

// formatNewsResponse formats the news items into a user-friendly response
func (f *GetNewsFromChinaNewsFunction) formatNewsResponse(newsItems []NewsItem, category string) string {
	var response strings.Builder

	categoryName := f.getCategoryDisplayName(category)
	response.WriteString(fmt.Sprintf("根据以下新闻信息回应用户的查询请求：\n\n"))
	response.WriteString(fmt.Sprintf("📰 中国新闻网 - %s新闻 (%d条)\n\n", categoryName, len(newsItems)))

	for i, item := range newsItems {
		response.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, item.Title))
		if item.Description != "" {
			response.WriteString(fmt.Sprintf("   %s\n", item.Description))
		}
		//response.WriteString(fmt.Sprintf("   🔗 %s\n", item.Link))
		//response.WriteString(fmt.Sprintf("   ⏰ %s\n\n", item.PubDate.Format("2006-01-02 15:04")))
	}

	response.WriteString("(请根据以上新闻信息，用自然、友好的语言向用户播报新闻内容，可以挑选用户可能感兴趣的重点新闻进行介绍)")

	return response.String()
}

// getCategoryDisplayName returns the display name for a category
func (f *GetNewsFromChinaNewsFunction) getCategoryDisplayName(category string) string {
	switch category {
	case "domestic", "国内":
		return "国内"
	case "international", "国际":
		return "国际"
	case "finance", "财经":
		return "财经"
	case "tech", "科技":
		return "科技"
	case "sports", "体育":
		return "体育"
	case "entertainment", "娱乐":
		return "娱乐"
	default:
		return "综合"
	}
}

// extractLinks extracts news links for potential follow-up
func (f *GetNewsFromChinaNewsFunction) extractLinks(newsItems []NewsItem) []string {
	var links []string
	for _, item := range newsItems {
		if item.Link != "" {
			links = append(links, item.Link)
		}
	}
	return links
}

// GetInfo returns the eino ToolInfo
func (f *GetNewsFromChinaNewsFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(GetNewsFromChinaNewsFunctionName, GetNewsFromChinaNewsFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", GetNewsFromChinaNewsFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType returns the tool type
func (f *GetNewsFromChinaNewsFunction) GetType() types.ToolType {
	return types.ToolTypeSystemCtl
}

// GetName returns the function name
func (f *GetNewsFromChinaNewsFunction) GetName() string {
	return GetNewsFromChinaNewsFunctionName
}

// GetDescription returns the function description
func (f *GetNewsFromChinaNewsFunction) GetDescription() interface{} {
	return GetNewsFromChinaNewsFunctionDesc
}

// RegisterGetNewsFromChinaNewsFunction registers the China News function
func RegisterGetNewsFromChinaNewsFunction() error {
	function := NewGetNewsFromChinaNewsFunction(nil)
	return tools.RegisterGlobalFunction(GetNewsFromChinaNewsFunctionName, function)
}

// init automatically registers the function when the package is imported
func init() {
	if err := RegisterGetNewsFromChinaNewsFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", GetNewsFromChinaNewsFunctionName, err)
	}
}
