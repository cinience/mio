package functions

import (
	"backend-server/internal/shared/ip_geo"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
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
	GetWeatherFunctionName = "get_weather"
)

// GetWeatherFunctionDesc defines the unified function description for LLM (supports both current weather and forecast)
var GetWeatherFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        GetWeatherFunctionName,
		"description": "获取指定地点的天气信息，支持当前天气查询和天气预报查询。不提供days参数时返回当前天气，提供days参数时返回天气预报",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "要查询天气的地点名称，如'北京'、'上海'、'广州'等",
				},
				"lang": map[string]interface{}{
					"type":        "string",
					"description": "返回语言，默认zh，支持zh、en",
					"default":     "zh",
				},
				"days": map[string]interface{}{
					"type":        "string",
					"description": "可选参数。预报天数，支持3d（3天预报）或7d（7天预报）。不提供此参数时返回当前天气和最近3天天气",
					"enum":        []string{"3d", "7d"},
				},
			},
			"required": []string{},
		},
	},
}

// WeatherResponse represents the weather API response structure
type WeatherResponse struct {
	Code string `json:"code"`
	Now  struct {
		ObsTime   string `json:"obsTime"`
		Temp      string `json:"temp"`
		FeelsLike string `json:"feelsLike"`
		Icon      string `json:"icon"`
		Text      string `json:"text"`
		Wind360   string `json:"wind360"`
		WindDir   string `json:"windDir"`
		WindScale string `json:"windScale"`
		WindSpeed string `json:"windSpeed"`
		Humidity  string `json:"humidity"`
		Precip    string `json:"precip"`
		Pressure  string `json:"pressure"`
		Vis       string `json:"vis"`
		Cloud     string `json:"cloud"`
		Dew       string `json:"dew"`
	} `json:"now"`
	Refer struct {
		Sources []string `json:"sources"`
		License []string `json:"license"`
	} `json:"refer"`
}

// WeatherForecastResponse represents the grid weather daily forecast API response structure
type WeatherForecastResponse struct {
	Code       string `json:"code"`
	UpdateTime string `json:"updateTime"`
	FxLink     string `json:"fxLink"`
	Daily      []struct {
		FxDate         string `json:"fxDate"`
		TempMax        string `json:"tempMax"`
		TempMin        string `json:"tempMin"`
		IconDay        string `json:"iconDay"`
		TextDay        string `json:"textDay"`
		IconNight      string `json:"iconNight"`
		TextNight      string `json:"textNight"`
		Wind360Day     string `json:"wind360Day"`
		WindDirDay     string `json:"windDirDay"`
		WindScaleDay   string `json:"windScaleDay"`
		WindSpeedDay   string `json:"windSpeedDay"`
		Wind360Night   string `json:"wind360Night"`
		WindDirNight   string `json:"windDirNight"`
		WindScaleNight string `json:"windScaleNight"`
		WindSpeedNight string `json:"windSpeedNight"`
		Humidity       string `json:"humidity"`
		Precip         string `json:"precip"`
		Pressure       string `json:"pressure"`
	} `json:"daily"`
	Refer struct {
		Sources []string `json:"sources"`
		License []string `json:"license"`
	} `json:"refer"`
}

// LocationResponse represents the location search API response
type LocationResponse struct {
	Code     string `json:"code"`
	Location []struct {
		Name      string `json:"name"`
		ID        string `json:"id"`
		Lat       string `json:"lat"`
		Lon       string `json:"lon"`
		Adm2      string `json:"adm2"`
		Adm1      string `json:"adm1"`
		Country   string `json:"country"`
		Tz        string `json:"tz"`
		UtcOffset string `json:"utcOffset"`
		IsDst     string `json:"isDst"`
		Type      string `json:"type"`
		Rank      string `json:"rank"`
		FxLink    string `json:"fxLink"`
	} `json:"location"`
}

// GetWeatherFunction implements the weather query function
type GetWeatherFunction struct {
	config map[string]interface{}
}

// NewGetWeatherFunction creates a new weather function
func NewGetWeatherFunction(args map[string]interface{}) *GetWeatherFunction {
	return &GetWeatherFunction{
		config: args,
	}
}

// Execute implements the unified function execution (supports both current weather and forecast)
func (f *GetWeatherFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}
	// Only add new keys, don't overwrite existing ones
	for key, value := range f.config {
		if _, exists := args[key]; !exists {
			args[key] = value
		}
	}
	// Extract parameters
	lang, _ := args["lang"].(string)
	days, _ := args["days"].(string)
	if lang == "" {
		lang = "zh"
	}

	// Get API configuration with defaults
	apiHost, _ := args["api_host"].(string)
	apiKey, _ := args["api_key"].(string)

	location, clientIP := f.getLocation(ctx, conn, args)
	// Determine if this is a forecast request or current weather request
	isForecast := days != "" && (days == "3d" || days == "7d")

	if isForecast {
		if logger != nil {
			logger.Info("查询天气预报", "location", location, "lang", lang, "days", days, "ip", clientIP)
		}
		return f.executeForecast(ctx, conn, location, lang, days, apiHost, apiKey, logger)
	} else {
		if logger != nil {
			logger.Info("查询当前天气", "location", location, "lang", lang, "ip", clientIP)
		}
		return f.executeCurrentWeather(ctx, conn, location, lang, apiHost, apiKey, logger)
	}
}

// executeCurrentWeather handles current weather queries
func (f *GetWeatherFunction) executeCurrentWeather(ctx context.Context, conn types.Connection, location, lang, apiHost, apiKey string, logger *slog.Logger) (*types.ActionResponse, error) {
	// Try to get weather data from cache first
	cacheKey := fmt.Sprintf("weather_%s_%s", location, lang)
	if cachedData, found := utils.GetCache(utils.CacheTypeWeather, cacheKey); found {
		if logger != nil {
			logger.Debug("从缓存获取天气数据", "location", location)
		}
		return types.NewActionResponse(types.ActionReqLLM, cachedData, nil), nil
	}

	// Get location ID first
	locationID, err := f.getLocationID(ctx, apiHost, apiKey, location)
	if err != nil {
		if logger != nil {
			logger.Error("获取地点ID失败", "error", err)
		}
		log.Errorf("获取地点ID失败: %v", err)
		return types.NewActionResponse(types.ActionReqLLM, "抱歉，无法找到指定地点的天气信息", nil), nil
	}

	// Get weather data
	weatherData, err := f.getWeatherData(ctx, apiHost, apiKey, locationID, lang)
	if err != nil {
		if logger != nil {
			logger.Error("获取天气数据失败", "error", err)
		}
		log.Errorf("获取天气数据失败: %v", err)
		return types.NewActionResponse(types.ActionReqLLM, "抱歉，获取天气信息失败，请稍后再试", nil), nil
	}

	// Format weather response
	response := f.formatWeatherResponse(weatherData, location, lang)

	forecastData, err := f.getWeatherForecastData(ctx, apiHost, apiKey, locationID, lang, "3")
	if err != nil {
		log.Errorf("获取天气预报数据失败: %v", err)
	}

	if forecastData != nil {
		response = response + f.formatWeatherForecastResponse(forecastData, location, lang, "3")
	}

	// Cache the response
	utils.SetCacheWithDefaultTTL(utils.CacheTypeWeather, cacheKey, response)

	if logger != nil {
		logger.Info("天气查询成功", "location", location)
	}

	return types.NewActionResponse(types.ActionReqLLM, response, nil), nil
}

func (f *GetWeatherFunction) getLocation(ctx context.Context, conn types.Connection, args map[string]interface{}) (string, string) {
	location, _ := args["location"].(string)

	if location != "" {
		return location, ""
	}

	// Get client IP for location detection if needed
	var clientIP string
	if conn != nil {
		clientIP, _, _ = net.SplitHostPort(conn.GetRemoteAddr())
	}
	if clientIP != "" {
		geo, err := ip_geo.IPGeoGet(clientIP)
		if err != nil {
			log.Errorf("获取IP[%s]地理位置失败: %v", clientIP, err)
		}
		if geo != nil {
			location = geo.City
		}
	}

	if location != "" {
		return location, clientIP
	}

	defaultLocation, _ := args["defaultLocation"].(string)
	if defaultLocation == "" {
		defaultLocation = "北京"
	}

	return defaultLocation, clientIP
}

// executeForecast handles weather forecast queries
func (f *GetWeatherFunction) executeForecast(ctx context.Context, conn types.Connection, location, lang, days, apiHost, apiKey string, logger *slog.Logger) (*types.ActionResponse, error) {
	// Try to get weather forecast data from cache first
	cacheKey := fmt.Sprintf("weather_forecast_%s_%s_%s", location, lang, days)
	if cachedData, found := utils.GetCache(utils.CacheTypeWeather, cacheKey); found {
		if logger != nil {
			logger.Debug("从缓存获取天气预报数据", "location", location)
		}
		return types.NewActionResponse(types.ActionReqLLM, cachedData, nil), nil
	}

	// Get location coordinates first
	locationCoords, err := f.getLocationCoordinates(ctx, apiHost, apiKey, location)
	if err != nil {
		if logger != nil {
			logger.Error("获取地点坐标失败", "error", err)
		}
		fmt.Println("获取地点坐标失败: ", err)
		return types.NewActionResponse(types.ActionReqLLM, "抱歉，无法找到指定地点的天气预报信息", nil), nil
	}

	// Get weather forecast data
	forecastData, err := f.getWeatherForecastData(ctx, apiHost, apiKey, locationCoords, lang, days)
	if err != nil {
		if logger != nil {
			logger.Error("获取天气预报数据失败", "error", err)
		}
		return types.NewActionResponse(types.ActionReqLLM, "抱歉，获取天气预报信息失败，请稍后再试", nil), nil
	}

	// Format weather forecast response
	response := f.formatWeatherForecastResponse(forecastData, location, lang, days)

	// Cache the response
	utils.SetCacheWithDefaultTTL(utils.CacheTypeWeather, cacheKey, response)

	if logger != nil {
		logger.Info("天气预报查询成功", "location", location)
	}

	return types.NewActionResponse(types.ActionReqLLM, response, nil), nil
}

// getLocationID searches for location ID using the location name
func (f *GetWeatherFunction) getLocationID(ctx context.Context, apiHost, apiKey, location string) (string, error) {
	// Build location search URL
	searchURL := fmt.Sprintf("https://%s/geo/v2/city/lookup?location=%s&key=%s",
		apiHost, url.QueryEscape(location), apiKey)

	// Make HTTP request
	headers := map[string]string{
		"User-Agent": "XiaoZhi-Weather/1.0",
	}

	body, err := utils.Get(ctx, searchURL, headers)
	if err != nil {
		return "", fmt.Errorf("location search request failed: %v", err)
	}

	// Parse response
	var locationResp LocationResponse
	if err := json.Unmarshal(body, &locationResp); err != nil {
		return "", fmt.Errorf("failed to parse location response: %v", err)
	}

	// Check response code
	if locationResp.Code != "200" {
		return "", fmt.Errorf("location search failed with code: %s", locationResp.Code)
	}

	// Check if any locations found
	if len(locationResp.Location) == 0 {
		return "", fmt.Errorf("no locations found for: %s", location)
	}

	// Return the first location ID
	return locationResp.Location[0].ID, nil
}

// getWeatherData fetches current weather data for the given location ID
func (f *GetWeatherFunction) getWeatherData(ctx context.Context, apiHost, apiKey, locationID, lang string) (*WeatherResponse, error) {
	// Build weather API URL
	weatherURL := fmt.Sprintf("https://%s/v7/weather/now?location=%s&key=%s&lang=%s",
		apiHost, locationID, apiKey, lang)

	// Make HTTP request
	headers := map[string]string{
		"User-Agent": "XiaoZhi-Weather/1.0",
	}

	body, err := utils.Get(ctx, weatherURL, headers)
	if err != nil {
		return nil, fmt.Errorf("weather request failed: %v", err)
	}

	// Parse response
	var weatherResp WeatherResponse
	if err := json.Unmarshal(body, &weatherResp); err != nil {
		return nil, fmt.Errorf("failed to parse weather response: %v", err)
	}

	// Check response code
	if weatherResp.Code != "200" {
		return nil, fmt.Errorf("weather request failed with code: %s", weatherResp.Code)
	}

	return &weatherResp, nil
}

// formatWeatherResponse formats the weather data into a user-friendly response
func (f *GetWeatherFunction) formatWeatherResponse(weather *WeatherResponse, location, lang string) string {
	var response strings.Builder

	if lang == "zh" || strings.HasPrefix(lang, "zh") {
		response.WriteString(fmt.Sprintf("根据以下天气信息回应用户的查询请求：\n\n"))
		response.WriteString(fmt.Sprintf("地点：%s\n", location))
		response.WriteString(fmt.Sprintf("当前温度：%s°C\n", weather.Now.Temp))
		response.WriteString(fmt.Sprintf("天气状况：%s\n", weather.Now.Text))
		response.WriteString(fmt.Sprintf("体感温度：%s°C\n", weather.Now.FeelsLike))
		response.WriteString(fmt.Sprintf("风向风力：%s %s级 (%s km/h)\n", weather.Now.WindDir, weather.Now.WindScale, weather.Now.WindSpeed))
		//response.WriteString(fmt.Sprintf("💧 相对湿度：%s%%\n", weather.Now.Humidity))
		//response.WriteString(fmt.Sprintf("🌧️ 降水量：%s mm\n", weather.Now.Precip))
		//response.WriteString(fmt.Sprintf("📊 大气压强：%s hPa\n", weather.Now.Pressure))
		//response.WriteString(fmt.Sprintf("👁️ 能见度：%s km\n", weather.Now.Vis))
		//response.WriteString(fmt.Sprintf("☁️ 云量：%s%%\n", weather.Now.Cloud))
		//response.WriteString(fmt.Sprintf("💦 露点温度：%s°C\n", weather.Now.Dew))
		//response.WriteString(fmt.Sprintf("\n⏰ 观测时间：%s\n", weather.Now.ObsTime))
		response.WriteString("\n(请根据以上天气信息，用自然、友好的语言向用户播报天气情况, 默认只需要播报当前温度和天气状况)")
	} else {
		response.WriteString(fmt.Sprintf("Weather information for user query:\n\n"))
		response.WriteString(fmt.Sprintf("📍 Location: %s\n", location))
		response.WriteString(fmt.Sprintf("🌡️ Temperature: %s°C\n", weather.Now.Temp))
		response.WriteString(fmt.Sprintf("🌤️ Condition: %s\n", weather.Now.Text))
		response.WriteString(fmt.Sprintf("🤗 Feels like: %s°C\n", weather.Now.FeelsLike))
		response.WriteString(fmt.Sprintf("💨 Wind: %s %s scale (%s km/h)\n", weather.Now.WindDir, weather.Now.WindScale, weather.Now.WindSpeed))
		response.WriteString(fmt.Sprintf("💧 Humidity: %s%%\n", weather.Now.Humidity))
		response.WriteString(fmt.Sprintf("🌧️ Precipitation: %s mm\n", weather.Now.Precip))
		response.WriteString(fmt.Sprintf("📊 Pressure: %s hPa\n", weather.Now.Pressure))
		response.WriteString(fmt.Sprintf("👁️ Visibility: %s km\n", weather.Now.Vis))
		response.WriteString(fmt.Sprintf("☁️ Cloud cover: %s%%\n", weather.Now.Cloud))
		response.WriteString(fmt.Sprintf("💦 Dew point: %s°C\n", weather.Now.Dew))
		//response.WriteString(fmt.Sprintf("\n⏰ Observation time: %s\n", weather.Now.ObsTime))
		response.WriteString("\n(Please provide weather information to user in a natural, friendly manner)")
	}

	return response.String()
}

// getLocationCoordinates searches for location coordinates using the location name (for forecast API)
func (f *GetWeatherFunction) getLocationCoordinates(ctx context.Context, apiHost, apiKey, location string) (string, error) {
	// Build location search URL
	searchURL := fmt.Sprintf("https://%s/geo/v2/city/lookup?location=%s&key=%s",
		apiHost, url.QueryEscape(location), apiKey)

	// Make HTTP request
	headers := map[string]string{
		"User-Agent": "Weather/1.0",
	}

	body, err := utils.Get(ctx, searchURL, headers)
	if err != nil {
		return "", fmt.Errorf("location search request failed: %v", err)
	}

	// Parse response
	var locationResp LocationResponse
	if err := json.Unmarshal(body, &locationResp); err != nil {
		return "", fmt.Errorf("failed to parse location response: %v", err)
	}

	// Check response code
	if locationResp.Code != "200" {
		return "", fmt.Errorf("location search failed with code: %s", locationResp.Code)
	}

	// Check if any locations found
	if len(locationResp.Location) == 0 {
		return "", fmt.Errorf("no locations found for: %s", location)
	}

	// Return coordinates in "lon,lat" format
	loc := locationResp.Location[0]
	return fmt.Sprintf("%s,%s", loc.Lon, loc.Lat), nil
}

// getWeatherForecastData fetches weather forecast data for the given coordinates
func (f *GetWeatherFunction) getWeatherForecastData(ctx context.Context, apiHost, apiKey, coordinates, lang, days string) (*WeatherForecastResponse, error) {
	// Build weather forecast API URL
	forecastURL := fmt.Sprintf("https://%s/v7/grid-weather/%s?location=%s&key=%s&lang=%s",
		apiHost, days, coordinates, apiKey, lang)

	// Make HTTP request
	headers := map[string]string{
		"User-Agent": "Weather/1.0",
	}

	body, err := utils.Get(ctx, forecastURL, headers)
	if err != nil {
		return nil, fmt.Errorf("weather forecast request failed: %v", err)
	}

	// Parse response
	var forecastResp WeatherForecastResponse
	if err := json.Unmarshal(body, &forecastResp); err != nil {
		return nil, fmt.Errorf("failed to parse weather forecast response: %v", err)
	}

	// Check response code
	if forecastResp.Code != "200" {
		return nil, fmt.Errorf("weather forecast request failed with code: %s", forecastResp.Code)
	}

	return &forecastResp, nil
}

// formatWeatherForecastResponse formats the weather forecast data into a user-friendly response
func (f *GetWeatherFunction) formatWeatherForecastResponse(forecast *WeatherForecastResponse, location, lang, days string) string {
	var response strings.Builder

	if lang == "zh" || strings.HasPrefix(lang, "zh") {
		response.WriteString(fmt.Sprintf("根据以下天气预报信息回应用户的查询请求：\n\n"))
		response.WriteString(fmt.Sprintf(" 地点：%s\n", location))
		response.WriteString(fmt.Sprintf(" 预报天数：%s\n", days))
		response.WriteString(fmt.Sprintf(" 更新时间：%s\n\n", forecast.UpdateTime))
		response.WriteString(fmt.Sprintf(" 当前时间：%s\n\n", time.Now().Format("2006-01-02 15:04")))

		for i, daily := range forecast.Daily {
			dayNum := i + 1
			if dayNum == 1 {
				continue
				//response.WriteString(fmt.Sprintf("今天 (%s):\n", daily.FxDate))
			} else if dayNum == 2 {
				response.WriteString(fmt.Sprintf("明天 (%s):\n", daily.FxDate))
			} else if dayNum == 3 {
				response.WriteString(fmt.Sprintf("后天 (%s):\n", daily.FxDate))
			} else {
				response.WriteString(fmt.Sprintf("第%d天 (%s):\n", dayNum, daily.FxDate))
			}
			response.WriteString(fmt.Sprintf("  白天：%s\n", daily.TextDay))
			response.WriteString(fmt.Sprintf("  夜间：%s\n", daily.TextNight))
			response.WriteString(fmt.Sprintf("  温度：%s度到%s度\n", daily.TempMin, daily.TempMax))
			response.WriteString(fmt.Sprintf("  白天%s %s级，夜间%s %s级\n", daily.WindDirDay, daily.WindScaleDay, daily.WindDirNight, daily.WindScaleNight))
			//response.WriteString(fmt.Sprintf("  💧 湿度：%s%%\n", daily.Humidity))
			//response.WriteString(fmt.Sprintf("  🌧️ 降水量：%s mm\n", daily.Precip))
			//response.WriteString(fmt.Sprintf("  📊 气压：%s hPa\n\n", daily.Pressure))
		}
		response.WriteString("(请根据以上天气预报信息，用自然、友好的语言向用户播报未来几天的天气情况, 默认只需要播报天气状况和最高最低温度)")
	} else {
		response.WriteString(fmt.Sprintf("Weather forecast information for user query:\n\n"))
		response.WriteString(fmt.Sprintf("Location: %s\n", location))
		response.WriteString(fmt.Sprintf("Forecast days: %s\n", days))
		response.WriteString(fmt.Sprintf("Update time: %s\n\n", forecast.UpdateTime))

		for i, daily := range forecast.Daily {
			dayNum := i + 1
			response.WriteString(fmt.Sprintf(" Day %d (%s):\n", dayNum, daily.FxDate))
			response.WriteString(fmt.Sprintf(" Temperature: %s°C ~ %s°C\n", daily.TempMin, daily.TempMax))
			response.WriteString(fmt.Sprintf(" Day: %s\n", daily.TextDay))
			response.WriteString(fmt.Sprintf(" Night: %s\n", daily.TextNight))
			response.WriteString(fmt.Sprintf(" Wind: Day %s %s scale, Night %s %s scale\n",
				daily.WindDirDay, daily.WindScaleDay, daily.WindDirNight, daily.WindScaleNight))
			response.WriteString(fmt.Sprintf(" Humidity: %s%%\n", daily.Humidity))
			response.WriteString(fmt.Sprintf(" Precipitation: %s mm\n", daily.Precip))
			response.WriteString(fmt.Sprintf(" ressure: %s hPa\n\n", daily.Pressure))
		}

		response.WriteString("(Please provide weather forecast information to user in a natural, friendly manner)")
	}

	return response.String()
}

// GetInfo returns the eino ToolInfo
func (f *GetWeatherFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(GetWeatherFunctionName, GetWeatherFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", GetWeatherFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType returns the tool type
func (f *GetWeatherFunction) GetType() types.ToolType {
	return types.ToolTypeSystemCtl
}

// GetName returns the function name
func (f *GetWeatherFunction) GetName() string {
	return GetWeatherFunctionName
}

// GetDescription returns the function description
func (f *GetWeatherFunction) GetDescription() interface{} {
	return GetWeatherFunctionDesc
}

// RegisterGetWeatherFunction registers the weather function
func RegisterGetWeatherFunction() error {
	function := NewGetWeatherFunction(nil)
	return tools.RegisterGlobalFunction(GetWeatherFunctionName, function)
}

// init automatically registers the unified weather function when the package is imported
func init() {
	if err := RegisterGetWeatherFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", GetWeatherFunctionName, err)
	}
}
