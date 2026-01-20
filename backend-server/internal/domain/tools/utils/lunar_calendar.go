package utils

import (
	"fmt"
	"time"
)

// LunarCalendar provides lunar calendar calculations
// This is a simplified implementation of lunar calendar conversion
type LunarCalendar struct{}

// LunarDate represents a lunar calendar date
type LunarDate struct {
	Year        int    `json:"year"`
	Month       int    `json:"month"`
	Day         int    `json:"day"`
	IsLeapMonth bool   `json:"is_leap_month"`
	YearName    string `json:"year_name"`  // 天干地支年
	MonthName   string `json:"month_name"` // 农历月份名称
	DayName     string `json:"day_name"`   // 农历日期名称
	Zodiac      string `json:"zodiac"`     // 生肖
	Festival    string `json:"festival"`   // 节日
}

// SolarTerm represents a solar term (节气)
type SolarTerm struct {
	Name string    `json:"name"`
	Date time.Time `json:"date"`
}

// NewLunarCalendar creates a new lunar calendar instance
func NewLunarCalendar() *LunarCalendar {
	return &LunarCalendar{}
}

// SolarToLunar converts solar date to lunar date
func (lc *LunarCalendar) SolarToLunar(solarDate time.Time) *LunarDate {
	// This is a simplified implementation
	// In a real implementation, you would use a proper lunar calendar library
	// or implement the complex astronomical calculations

	year := solarDate.Year()
	month := int(solarDate.Month())
	day := solarDate.Day()

	// Simplified conversion (this is not accurate, just for demonstration)
	lunarYear := year
	lunarMonth := month
	lunarDay := day - 10 // Rough approximation

	if lunarDay <= 0 {
		lunarMonth--
		if lunarMonth <= 0 {
			lunarMonth = 12
			lunarYear--
		}
		lunarDay += 30 // Simplified month length
	}

	return &LunarDate{
		Year:        lunarYear,
		Month:       lunarMonth,
		Day:         lunarDay,
		IsLeapMonth: false,
		YearName:    lc.getYearName(lunarYear),
		MonthName:   lc.getMonthName(lunarMonth),
		DayName:     lc.getDayName(lunarDay),
		Zodiac:      lc.getZodiac(lunarYear),
		Festival:    lc.getFestival(lunarMonth, lunarDay),
	}
}

// getYearName returns the Chinese year name (天干地支)
func (lc *LunarCalendar) getYearName(year int) string {
	// 天干 (Heavenly Stems)
	heavenlyStems := []string{"甲", "乙", "丙", "丁", "戊", "己", "庚", "辛", "壬", "癸"}
	// 地支 (Earthly Branches)
	earthlyBranches := []string{"子", "丑", "寅", "卯", "辰", "巳", "午", "未", "申", "酉", "戌", "亥"}

	// Calculate position in 60-year cycle
	stemIndex := (year - 4) % 10
	branchIndex := (year - 4) % 12

	return heavenlyStems[stemIndex] + earthlyBranches[branchIndex]
}

// getMonthName returns the Chinese month name
func (lc *LunarCalendar) getMonthName(month int) string {
	monthNames := []string{
		"", "正月", "二月", "三月", "四月", "五月", "六月",
		"七月", "八月", "九月", "十月", "冬月", "腊月",
	}

	if month >= 1 && month <= 12 {
		return monthNames[month]
	}
	return fmt.Sprintf("%d月", month)
}

// getDayName returns the Chinese day name
func (lc *LunarCalendar) getDayName(day int) string {
	dayNames := map[int]string{
		1: "初一", 2: "初二", 3: "初三", 4: "初四", 5: "初五",
		6: "初六", 7: "初七", 8: "初八", 9: "初九", 10: "初十",
		11: "十一", 12: "十二", 13: "十三", 14: "十四", 15: "十五",
		16: "十六", 17: "十七", 18: "十八", 19: "十九", 20: "二十",
		21: "廿一", 22: "廿二", 23: "廿三", 24: "廿四", 25: "廿五",
		26: "廿六", 27: "廿七", 28: "廿八", 29: "廿九", 30: "三十",
	}

	if name, exists := dayNames[day]; exists {
		return name
	}
	return fmt.Sprintf("%d日", day)
}

// getZodiac returns the Chinese zodiac animal
func (lc *LunarCalendar) getZodiac(year int) string {
	zodiacs := []string{
		"鼠", "牛", "虎", "兔", "龙", "蛇",
		"马", "羊", "猴", "鸡", "狗", "猪",
	}

	return zodiacs[(year-4)%12]
}

// getFestival returns the festival name if any
func (lc *LunarCalendar) getFestival(month, day int) string {
	festivals := map[string]string{
		"1-1":   "春节",
		"1-15":  "元宵节",
		"2-2":   "龙抬头",
		"5-5":   "端午节",
		"7-7":   "七夕节",
		"7-15":  "中元节",
		"8-15":  "中秋节",
		"9-9":   "重阳节",
		"12-8":  "腊八节",
		"12-23": "小年",
		"12-30": "除夕",
	}

	key := fmt.Sprintf("%d-%d", month, day)
	if festival, exists := festivals[key]; exists {
		return festival
	}
	return ""
}

// GetSolarTerms returns the 24 solar terms for a given year
func (lc *LunarCalendar) GetSolarTerms(year int) []SolarTerm {
	// This is a simplified implementation
	// In reality, solar terms are calculated based on the sun's position
	solarTerms := []string{
		"立春", "雨水", "惊蛰", "春分", "清明", "谷雨",
		"立夏", "小满", "芒种", "夏至", "小暑", "大暑",
		"立秋", "处暑", "白露", "秋分", "寒露", "霜降",
		"立冬", "小雪", "大雪", "冬至", "小寒", "大寒",
	}

	var terms []SolarTerm
	baseDate := time.Date(year, 2, 4, 0, 0, 0, 0, time.UTC) // Approximate start

	for i, name := range solarTerms {
		// Approximate 15-day intervals
		termDate := baseDate.AddDate(0, 0, i*15)
		terms = append(terms, SolarTerm{
			Name: name,
			Date: termDate,
		})
	}

	return terms
}

// GetCurrentSolarTerm returns the current solar term
func (lc *LunarCalendar) GetCurrentSolarTerm(date time.Time) string {
	terms := lc.GetSolarTerms(date.Year())

	for i, term := range terms {
		if date.Before(term.Date) {
			if i > 0 {
				return terms[i-1].Name
			}
			// If before first term of year, return last term of previous year
			prevTerms := lc.GetSolarTerms(date.Year() - 1)
			return prevTerms[len(prevTerms)-1].Name
		}
	}

	// If after all terms, return the last one
	return terms[len(terms)-1].Name
}

// FormatLunarDate formats lunar date to Chinese string
func (lc *LunarCalendar) FormatLunarDate(lunar *LunarDate) string {
	var result string

	result += fmt.Sprintf("%s年", lunar.YearName)
	result += fmt.Sprintf("(%s年)", lunar.Zodiac)

	if lunar.IsLeapMonth {
		result += "闰"
	}
	result += lunar.MonthName
	result += lunar.DayName

	if lunar.Festival != "" {
		result += fmt.Sprintf(" (%s)", lunar.Festival)
	}

	return result
}

// GetTimeInfo returns comprehensive time information
func (lc *LunarCalendar) GetTimeInfo(date time.Time) map[string]interface{} {
	lunar := lc.SolarToLunar(date)
	solarTerm := lc.GetCurrentSolarTerm(date)

	return map[string]interface{}{
		"solar_date":    date.Format("2006年01月02日"),
		"solar_weekday": lc.getWeekdayName(date.Weekday()),
		"lunar_date":    lc.FormatLunarDate(lunar),
		"lunar_detail":  lunar,
		"solar_term":    solarTerm,
		"year_name":     lunar.YearName,
		"zodiac":        lunar.Zodiac,
		"festival":      lunar.Festival,
		"time_string":   date.Format("15:04:05"),
	}
}

// getWeekdayName returns Chinese weekday name
func (lc *LunarCalendar) getWeekdayName(weekday time.Weekday) string {
	weekdays := []string{
		"星期日", "星期一", "星期二", "星期三",
		"星期四", "星期五", "星期六",
	}
	return weekdays[int(weekday)]
}

// Global lunar calendar instance
var GlobalLunarCalendar = NewLunarCalendar()

// Convenience functions
func SolarToLunar(date time.Time) *LunarDate {
	return GlobalLunarCalendar.SolarToLunar(date)
}

func GetTimeInfo(date time.Time) map[string]interface{} {
	return GlobalLunarCalendar.GetTimeInfo(date)
}

func GetCurrentSolarTerm(date time.Time) string {
	return GlobalLunarCalendar.GetCurrentSolarTerm(date)
}
