package models

import (
	"encoding/json"
	"testing"
)

func TestRequestParse(t *testing.T) {
	// 测试用的 JSON 数据
	jsonData := `{"version":2,"language":"zh-CN","flash_size":16777216,"minimum_free_heap_size":8388160,"mac_address":"cc:ba:97:0b:93:94","uuid":"80e9d889-a7a0-404c-a55c-fae9681b737f","chip_model_name":"esp32s3","chip_info":{"model":9,"cores":2,"revision":2,"features":18},"application":{"name":"xiaozhi","version":"1.8.8","compile_time":"Aug 15 2025T03:20:12Z","idf_version":"v5.5","elf_sha256":"4d27aa5fc0b68b98bc1573fceb468b15e08e8da15fb56fcb1ba913d2b9cf24f1"},"partition_table": [{"label":"nvs","type":1,"subtype":2,"address":36864,"size":16384},{"label":"otadata","type":1,"subtype":0,"address":53248,"size":8192},{"label":"phy_init","type":1,"subtype":1,"address":61440,"size":4096},{"label":"model","type":1,"subtype":130,"address":65536,"size":983040},{"label":"ota_0","type":0,"subtype":16,"address":1048576,"size":6291456},{"label":"ota_1","type":0,"subtype":17,"address":7340032,"size":6291456}],"ota":{"label":"ota_0"},"board":{"type":"xingzhi-cube-0.96oled-wifi","name":"xingzhi-cube-0.96oled-wifi","ssid":"Xiaomi_8F5E","rssi":-42,"channel":1,"ip":"192.168.31.115","mac":"cc:ba:97:0b:93:94"}}`

	var req DeviceReportReqDTO
	err := json.Unmarshal([]byte(jsonData), &req)
	if err != nil {
		t.Errorf("Failed to unmarshal JSON: %v", err)
	}

	// 验证解析后的数据
	if req.Version != 2 {
		t.Errorf("Expected Version to be 2, got %d", req.Version)
	}

	if req.FlashSize != 16777216 {
		t.Errorf("Expected FlashSize to be 16777216, got %d", req.FlashSize)
	}

	if req.MinimumFreeHeapSize != 8388160 {
		t.Errorf("Expected MinimumFreeHeapSize to be 8388160, got %d", req.MinimumFreeHeapSize)
	}

	if req.MacAddress != "cc:ba:97:0b:93:94" {
		t.Errorf("Expected MacAddress to be \"cc:ba:97:0b:93:94\", got %s", req.MacAddress)
	}

	if req.UUID != "80e9d889-a7a0-404c-a55c-fae9681b737f" {
		t.Errorf("Expected UUID to be \"80e9d889-a7a0-404c-a55c-fae9681b737f\", got %s", req.UUID)
	}

	if req.ChipModelName != "esp32s3" {
		t.Errorf("Expected ChipModelName to be \"esp32s3\", got %s", req.ChipModelName)
	}

	// 验证嵌套的 ChipInfo
	if req.ChipInfo == nil {
		t.Error("Expected ChipInfo to be not nil")
	} else {
		if req.ChipInfo.Model != 9 {
			t.Errorf("Expected ChipInfo.Model to be 9, got %d", req.ChipInfo.Model)
		}
		if req.ChipInfo.Cores != 2 {
			t.Errorf("Expected ChipInfo.Cores to be 2, got %d", req.ChipInfo.Cores)
		}
	}

	// 验证嵌套的 Application
	if req.Application == nil {
		t.Error("Expected Application to be not nil")
	} else {
		if req.Application.Name != "xiaozhi" {
			t.Errorf("Expected Application.Name to be \"xiaozhi\", got %s", req.Application.Name)
		}
		if req.Application.Version != "1.8.8" {
			t.Errorf("Expected Application.Version to be \"1.8.8\", got %s", req.Application.Version)
		}
	}

	// 验证嵌套的 OTAInfo
	if req.OTA == nil {
		t.Error("Expected OTA to be not nil")
	} else {
		if req.OTA.Label != "ota_0" {
			t.Errorf("Expected OTA.Label to be \"ota_0\", got %s", req.OTA.Label)
		}
	}

	// 验证嵌套的 BoardInfo
	if req.Board == nil {
		t.Error("Expected Board to be not nil")
	} else {
		if req.Board.Type != "xingzhi-cube-0.96oled-wifi" {
			t.Errorf("Expected Board.Type to be \"xingzhi-cube-0.96oled-wifi\", got %s", req.Board.Type)
		}
		if req.Board.SSID != "Xiaomi_8F5E" {
			t.Errorf("Expected Board.SSID to be \"Xiaomi_8F5E\", got %s", req.Board.SSID)
		}
		if req.Board.RSSI != -42 {
			t.Errorf("Expected Board.RSSI to be -42, got %d", req.Board.RSSI)
		}
	}
}
