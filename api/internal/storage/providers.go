package storage

type Field struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Default  any      `json:"default,omitempty"`
	Options  []string `json:"options,omitempty"`
}

type Provider struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Implemented bool    `json:"implemented"`
	Fields      []Field `json:"fields"`
}

// Providers returns configuration schemas, not live adapters or auth sessions.
// Do not claim operational capabilities until a provider is implemented.
func Providers() []Provider {
	var result []Provider
	for _, item := range []struct{ id, name string }{{"115", "115 网盘"}, {"baidu", "百度网盘"}, {"quark", "夸克网盘"}, {"nas", "本地 NAS"}} {
		fields := []Field{{Key: "name", Label: "账号名称", Type: "text", Required: true}}
		if item.id == "115" || item.id == "baidu" {
			fields = append(fields, Field{Key: "accessToken", Label: "访问令牌", Type: "secret", Required: item.id == "115"}, Field{Key: "refreshToken", Label: "刷新令牌", Type: "secret", Required: true})
		}
		if item.id == "quark" {
			fields = append(fields, Field{Key: "cookie", Label: "Cookie", Type: "secret", Required: true})
		}
		root, download, cache := "0", "proxy", 30
		if item.id == "115" {
			download = "redirect"
		}
		if item.id == "baidu" {
			root = "/"
		}
		if item.id == "nas" {
			root = ""
			cache = 0
		} else {
			fields = append(fields, Field{Key: "downloadMode", Label: "下载模式", Type: "select", Default: download, Options: []string{"redirect", "proxy"}}, Field{Key: "deleteMode", Label: "删除模式", Type: "select", Default: "trash", Options: []string{"trash", "permanent"}})
		}
		rootLabel := "根目录 ID"
		if item.id == "nas" {
			rootLabel = "本地根目录"
		} else if item.id == "baidu" {
			rootLabel = "根目录路径"
		}
		fields = append(fields, Field{Key: "root", Label: rootLabel, Type: "text", Required: true, Default: root}, Field{Key: "cacheMinutes", Label: "缓存时间（分钟）", Type: "integer", Default: cache})
		result = append(result, Provider{ID: item.id, Name: item.name, Fields: fields})
	}
	return result
}
