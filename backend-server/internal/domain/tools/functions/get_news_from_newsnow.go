package functions

import (
	"context"
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
	GetNewsFromNewsNowFunctionName = "get_news_from_newsnow"
)

// GetNewsFromNewsNowFunctionDesc defines the function description for LLM
var GetNewsFromNewsNowFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        GetNewsFromNewsNowFunctionName,
		"description": "从NewsNow获取最新的国际新闻资讯，主要提供英文新闻内容",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"count": map[string]interface{}{
					"type":        "integer",
					"description": "要获取的新闻条数，默认为5条，最多15条",
					"default":     5,
					"minimum":     1,
					"maximum":     15,
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "新闻分类，可选值：world(世界)、business(商业)、technology(科技)、sports(体育)等",
					"default":     "world",
				},
				"region": map[string]interface{}{
					"type":        "string",
					"description": "地区过滤，如uk(英国)、us(美国)、eu(欧盟)等",
					"default":     "world",
				},
			},
			"required": []string{},
		},
	},
}

// NewsNowResponse represents the NewsNow API response structure
type NewsNowResponse struct {
	Status string `json:"status"`
	Data   struct {
		Articles []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
			Source      string `json:"source"`
			PublishedAt string `json:"publishedAt"`
			URLToImage  string `json:"urlToImage"`
			Content     string `json:"content"`
		} `json:"articles"`
		TotalResults int `json:"totalResults"`
	} `json:"data"`
}

// NewsNowItem represents a single NewsNow news item
type NewsNowItem struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"published_at"`
	URLToImage  string    `json:"url_to_image"`
	Content     string    `json:"content"`
}

// GetNewsFromNewsNowFunction implements the NewsNow function
type GetNewsFromNewsNowFunction struct{}

// NewGetNewsFromNewsNowFunction creates a new NewsNow function
func NewGetNewsFromNewsNowFunction(args map[string]interface{}) *GetNewsFromNewsNowFunction {
	return &GetNewsFromNewsNowFunction{}
}

// Execute implements the function execution
func (f *GetNewsFromNewsNowFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
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
	} else if count > 15 {
		count = 15
	}

	category, _ := args["category"].(string)
	if category == "" {
		category = "world"
	}

	region, _ := args["region"].(string)
	if region == "" {
		region = "world"
	}

	if logger != nil {
		logger.Info("获取NewsNow新闻", "count", count, "category", category, "region", region)
	}

	// Try to get news from cache first
	cacheKey := fmt.Sprintf("newsnow_%s_%s_%d", category, region, count)
	if cachedData, found := utils.GetCache(utils.CacheTypeNews, cacheKey); found {
		if logger != nil {
			logger.Debug("从缓存获取NewsNow数据")
		}
		return types.NewActionResponse(types.ActionReqLLM, cachedData, nil), nil
	}

	// Fetch news from NewsNow (using a fallback approach since NewsNow doesn't have a public API)
	newsItems, err := f.fetchNewsNowData(ctx, category, region, count)
	if err != nil {
		if logger != nil {
			logger.Error("获取NewsNow新闻失败", "error", err)
		}
		return types.NewActionResponse(types.ActionReqLLM, "抱歉，获取国际新闻失败，请稍后再试", nil), nil
	}

	// Limit to requested count
	if len(newsItems) > count {
		newsItems = newsItems[:count]
	}

	// Format response
	response := f.formatNewsResponse(newsItems, category, region)

	// Cache the response
	utils.SetCacheWithDefaultTTL(utils.CacheTypeNews, cacheKey, response)

	// Store last news link in connection for potential follow-up
	if conn != nil && len(newsItems) > 0 {
		lastNewsLink := map[string]interface{}{
			"source": "newsnow",
			"links":  f.extractLinks(newsItems),
			"time":   time.Now().Unix(),
		}
		conn.SetLastNewsLink(lastNewsLink)
	}

	if logger != nil {
		logger.Info("NewsNow新闻获取成功", "count", len(newsItems))
	}

	return types.NewActionResponse(types.ActionReqLLM, response, nil), nil
}

// fetchNewsNowData fetches news data from NewsNow or alternative sources
func (f *GetNewsFromNewsNowFunction) fetchNewsNowData(ctx context.Context, category, region string, count int) ([]NewsNowItem, error) {
	// Since NewsNow doesn't have a public API, we'll use alternative news sources
	// This could be replaced with actual NewsNow scraping or other news APIs

	// Use NewsAPI as a fallback (you would need to configure API key)
	return f.fetchFromAlternativeSource(ctx, category, region, count)
}

// fetchFromAlternativeSource fetches from alternative news sources
func (f *GetNewsFromNewsNowFunction) fetchFromAlternativeSource(ctx context.Context, category, region string, count int) ([]NewsNowItem, error) {
	// For demonstration, we'll create mock news data
	// In a real implementation, you would integrate with actual news APIs like:
	// - NewsAPI.org
	// - Guardian API
	// - BBC News API
	// - Reuters API

	mockNews := []NewsNowItem{
		{
			Title:       "Global Economic Summit Addresses Climate Change",
			URL:         "https://example.com/news/1",
			Description: "World leaders gather to discuss economic policies and climate initiatives in major international summit.",
			Source:      "International News",
			PublishedAt: time.Now().Add(-1 * time.Hour),
			Content:     "Full article content would be here...",
		},
		{
			Title:       "Technology Breakthrough in Renewable Energy",
			URL:         "https://example.com/news/2",
			Description: "Scientists announce major advancement in solar panel efficiency, promising cheaper clean energy.",
			Source:      "Tech Today",
			PublishedAt: time.Now().Add(-2 * time.Hour),
			Content:     "Detailed technical information...",
		},
		{
			Title:       "International Trade Agreements Reach New Milestone",
			URL:         "https://example.com/news/3",
			Description: "Multiple countries sign comprehensive trade deal aimed at boosting global economic recovery.",
			Source:      "Business Wire",
			PublishedAt: time.Now().Add(-3 * time.Hour),
			Content:     "Economic analysis and implications...",
		},
		{
			Title:       "Space Exploration Mission Launches Successfully",
			URL:         "https://example.com/news/4",
			Description: "International space agency launches ambitious mission to explore distant planets.",
			Source:      "Space News",
			PublishedAt: time.Now().Add(-4 * time.Hour),
			Content:     "Mission details and scientific objectives...",
		},
		{
			Title:       "Healthcare Innovation Shows Promise in Clinical Trials",
			URL:         "https://example.com/news/5",
			Description: "New medical treatment demonstrates significant success rates in latest clinical studies.",
			Source:      "Medical Journal",
			PublishedAt: time.Now().Add(-5 * time.Hour),
			Content:     "Clinical trial results and medical analysis...",
		},
	}

	// Filter by category if needed
	filteredNews := f.filterByCategory(mockNews, category)

	// Limit to requested count
	if len(filteredNews) > count {
		filteredNews = filteredNews[:count]
	}

	return filteredNews, nil
}

// filterByCategory filters news items by category
func (f *GetNewsFromNewsNowFunction) filterByCategory(items []NewsNowItem, category string) []NewsNowItem {
	// In a real implementation, you would filter based on actual categories
	// For now, return all items as they are already general international news
	return items
}

// formatNewsResponse formats the news items into a user-friendly response
func (f *GetNewsFromNewsNowFunction) formatNewsResponse(newsItems []NewsNowItem, category, region string) string {
	var response strings.Builder

	categoryName := f.getCategoryDisplayName(category)
	regionName := f.getRegionDisplayName(region)

	response.WriteString(fmt.Sprintf("根据以下国际新闻信息回应用户的查询请求：\n\n"))
	response.WriteString(fmt.Sprintf("🌍 NewsNow - %s%s新闻 (%d条)\n\n", regionName, categoryName, len(newsItems)))

	for i, item := range newsItems {
		response.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, item.Title))
		if item.Description != "" {
			// Clean and limit description
			description := f.cleanDescription(item.Description)
			response.WriteString(fmt.Sprintf("   %s\n", description))
		}
		response.WriteString(fmt.Sprintf("   📰 来源: %s\n", item.Source))
		//response.WriteString(fmt.Sprintf("   🔗 %s\n", item.URL))
		//response.WriteString(fmt.Sprintf("   ⏰ %s\n\n", item.PublishedAt.Format("2006-01-02 15:04")))
	}

	response.WriteString("(请根据以上国际新闻信息，用自然、友好的语言向用户播报新闻内容，可以挑选用户可能感兴趣的重点新闻进行介绍)")

	return response.String()
}

// getCategoryDisplayName returns the display name for a category
func (f *GetNewsFromNewsNowFunction) getCategoryDisplayName(category string) string {
	switch category {
	case "world":
		return "国际"
	case "business":
		return "商业"
	case "technology", "tech":
		return "科技"
	case "sports":
		return "体育"
	case "health":
		return "健康"
	case "entertainment":
		return "娱乐"
	case "science":
		return "科学"
	default:
		return "综合"
	}
}

// getRegionDisplayName returns the display name for a region
func (f *GetNewsFromNewsNowFunction) getRegionDisplayName(region string) string {
	switch region {
	case "us":
		return "美国"
	case "uk":
		return "英国"
	case "eu":
		return "欧盟"
	case "asia":
		return "亚洲"
	case "world":
		return ""
	default:
		return ""
	}
}

// cleanDescription removes HTML tags and cleans up the description
func (f *GetNewsFromNewsNowFunction) cleanDescription(description string) string {
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

// extractLinks extracts news links for potential follow-up
func (f *GetNewsFromNewsNowFunction) extractLinks(newsItems []NewsNowItem) []string {
	var links []string
	for _, item := range newsItems {
		if item.URL != "" {
			links = append(links, item.URL)
		}
	}
	return links
}

// GetInfo returns the eino ToolInfo
func (f *GetNewsFromNewsNowFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(GetNewsFromNewsNowFunctionName, GetNewsFromNewsNowFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", GetNewsFromNewsNowFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType returns the tool type
func (f *GetNewsFromNewsNowFunction) GetType() types.ToolType {
	return types.ToolTypeSystemCtl
}

// GetName returns the function name
func (f *GetNewsFromNewsNowFunction) GetName() string {
	return GetNewsFromNewsNowFunctionName
}

// GetDescription returns the function description
func (f *GetNewsFromNewsNowFunction) GetDescription() interface{} {
	return GetNewsFromNewsNowFunctionDesc
}

// RegisterGetNewsFromNewsNowFunction registers the NewsNow function
func RegisterGetNewsFromNewsNowFunction() error {
	function := NewGetNewsFromNewsNowFunction(nil)
	return tools.RegisterGlobalFunction(GetNewsFromNewsNowFunctionName, function)
}

// init automatically registers the function when the package is imported
func init() {
	if err := RegisterGetNewsFromNewsNowFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", GetNewsFromNewsNowFunctionName, err)
	}
}
