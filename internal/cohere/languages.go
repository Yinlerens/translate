package cohere

type Language struct {
	Code string `json:"语言代码"`
	Name string `json:"名称"`
}

// Codes follow Cohere's supported language list; names are localized for clients.
var languages = []Language{
	{"sq", "阿尔巴尼亚语"}, {"ar-EG", "阿拉伯语（埃及）"}, {"ar", "阿拉伯语（现代标准）"}, {"ar-SA", "阿拉伯语（沙特）"},
	{"bn", "孟加拉语"}, {"bg", "保加利亚语"}, {"ca", "加泰罗尼亚语"}, {"zh-CN", "简体中文"}, {"zh-TW", "繁体中文"},
	{"hr", "克罗地亚语"}, {"cs", "捷克语"}, {"da", "丹麦语"}, {"nl", "荷兰语"}, {"en", "英语"}, {"et", "爱沙尼亚语"},
	{"fil", "菲律宾语"}, {"fi", "芬兰语"}, {"fr", "法语"}, {"de", "德语"}, {"el", "希腊语"}, {"he", "希伯来语"},
	{"hi", "印地语"}, {"hu", "匈牙利语"}, {"is", "冰岛语"}, {"id", "印度尼西亚语"}, {"ga", "爱尔兰语"}, {"it", "意大利语"},
	{"ja", "日语"}, {"ko", "韩语"}, {"lv", "拉脱维亚语"}, {"lt", "立陶宛语"}, {"ms", "马来语"}, {"mt", "马耳他语"},
	{"nb", "挪威语（书面语）"}, {"fa", "波斯语"}, {"pl", "波兰语"}, {"pt-PT", "葡萄牙语（葡萄牙）"}, {"pa", "旁遮普语"},
	{"ro", "罗马尼亚语"}, {"ru", "俄语"}, {"sr-Cyrl", "塞尔维亚语（西里尔字母）"}, {"sk", "斯洛伐克语"}, {"sl", "斯洛文尼亚语"},
	{"es", "西班牙语"}, {"sv", "瑞典语"}, {"ta", "泰米尔语"}, {"te", "泰卢固语"}, {"th", "泰语"}, {"tr", "土耳其语"},
	{"uk", "乌克兰语"}, {"ur", "乌尔都语"}, {"vi", "越南语"},
}

func Languages() []Language { return append([]Language(nil), languages...) }
