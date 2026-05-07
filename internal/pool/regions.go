package pool

import "strings"

// regionInfo 描述一个 ISO 3166-1 alpha-2 地区代码、对应中文名以及常见识别别名。
type regionInfo struct {
	Code    string   // 二字母地区码（如 "HK"）
	Name    string   // 中文展示名（如 "香港"）
	Aliases []string // 常见识别别名（小写匹配；可包含中文别名）
}

// regions 是网关地区识别和分组展示用的权威数据源。
// 单一表替代之前分散的 defaultRegionGroupNames 与 regionNameAliases，
// 添加 / 修改地区时只需要改这里一处。
//
// 收录范围：常用商业代理出口地区 + 中文社区订阅常见地区。
// 不在表中的地区会被识别为 "UN"（未分组）。
var regions = []regionInfo{
	// 大中华
	{"HK", "香港", []string{"香港", "hong kong", "hongkong", "hkg"}},
	{"TW", "台湾", []string{"台湾", "臺灣", "taiwan", "taipei"}},
	{"MO", "澳门", []string{"澳门", "澳門", "macao", "macau"}},
	{"CN", "中国大陆", []string{"中国", "中国大陆", "中國", "china"}},

	// 东亚
	{"JP", "日本", []string{"日本", "japan", "tokyo", "osaka", "jpn"}},
	{"KR", "韩国", []string{"韩国", "韓國", "korea", "south korea", "seoul"}},

	// 东南亚
	{"SG", "新加坡", []string{"新加坡", "狮城", "singapore", "sgp"}},
	{"TH", "泰国", []string{"泰国", "泰國", "thailand", "bangkok"}},
	{"VN", "越南", []string{"越南", "vietnam", "viet nam"}},
	{"ID", "印尼", []string{"印尼", "印度尼西亚", "indonesia", "jakarta"}},
	{"MY", "马来西亚", []string{"马来", "馬來", "马来西亚", "malaysia", "kuala lumpur"}},
	{"PH", "菲律宾", []string{"菲律宾", "菲律賓", "philippines", "manila"}},
	{"KH", "柬埔寨", []string{"柬埔寨", "cambodia", "phnom penh"}},
	{"LA", "老挝", []string{"老挝", "寮國", "laos"}},
	{"MM", "缅甸", []string{"缅甸", "緬甸", "myanmar", "burma"}},
	{"BN", "文莱", []string{"文莱", "汶萊", "brunei"}},

	// 南亚
	{"IN", "印度", []string{"印度", "india", "mumbai", "delhi"}},
	{"PK", "巴基斯坦", []string{"巴基斯坦", "pakistan"}},
	{"BD", "孟加拉", []string{"孟加拉", "bangladesh"}},
	{"LK", "斯里兰卡", []string{"斯里兰卡", "斯里蘭卡", "sri lanka"}},

	// 北美
	{"US", "美国", []string{"美国", "美國", "united states", "america", "usa", "us"}},
	{"CA", "加拿大", []string{"加拿大", "canada", "toronto", "vancouver"}},
	{"MX", "墨西哥", []string{"墨西哥", "mexico"}},

	// 拉美
	{"BR", "巴西", []string{"巴西", "brazil", "brasil", "sao paulo"}},
	{"AR", "阿根廷", []string{"阿根廷", "argentina"}},
	{"CL", "智利", []string{"智利", "chile"}},
	{"CO", "哥伦比亚", []string{"哥伦比亚", "哥倫比亞", "colombia"}},
	{"PE", "秘鲁", []string{"秘鲁", "祕魯", "peru"}},

	// 欧洲（西欧）
	{"GB", "英国", []string{"英国", "英國", "united kingdom", "britain", "england", "uk", "london"}},
	{"IE", "爱尔兰", []string{"爱尔兰", "愛爾蘭", "ireland", "dublin"}},
	{"FR", "法国", []string{"法国", "法國", "france", "paris"}},
	{"DE", "德国", []string{"德国", "德國", "germany", "frankfurt", "berlin"}},
	{"NL", "荷兰", []string{"荷兰", "荷蘭", "netherlands", "holland", "amsterdam"}},
	{"BE", "比利时", []string{"比利时", "比利時", "belgium"}},
	{"LU", "卢森堡", []string{"卢森堡", "盧森堡", "luxembourg"}},

	// 欧洲（北欧）
	{"SE", "瑞典", []string{"瑞典", "sweden", "stockholm"}},
	{"NO", "挪威", []string{"挪威", "norway"}},
	{"FI", "芬兰", []string{"芬兰", "芬蘭", "finland", "helsinki"}},
	{"DK", "丹麦", []string{"丹麦", "丹麥", "denmark"}},
	{"IS", "冰岛", []string{"冰岛", "冰島", "iceland"}},

	// 欧洲（南欧）
	{"ES", "西班牙", []string{"西班牙", "spain", "madrid"}},
	{"PT", "葡萄牙", []string{"葡萄牙", "portugal", "lisbon"}},
	{"IT", "意大利", []string{"意大利", "italy", "rome", "milan"}},
	{"GR", "希腊", []string{"希腊", "希臘", "greece", "athens"}},

	// 欧洲（中欧）
	{"AT", "奥地利", []string{"奥地利", "奧地利", "austria"}},
	{"CH", "瑞士", []string{"瑞士", "switzerland", "zurich"}},
	{"PL", "波兰", []string{"波兰", "波蘭", "poland"}},
	{"CZ", "捷克", []string{"捷克", "czech", "czechia"}},
	{"HU", "匈牙利", []string{"匈牙利", "hungary"}},
	{"SK", "斯洛伐克", []string{"斯洛伐克", "slovakia"}},
	{"SI", "斯洛文尼亚", []string{"斯洛文尼亚", "slovenia"}},
	{"HR", "克罗地亚", []string{"克罗地亚", "克羅地亞", "croatia"}},
	{"RO", "罗马尼亚", []string{"罗马尼亚", "羅馬尼亞", "romania"}},
	{"BG", "保加利亚", []string{"保加利亚", "保加利亞", "bulgaria"}},
	{"RS", "塞尔维亚", []string{"塞尔维亚", "塞爾維亞", "serbia"}},
	{"UA", "乌克兰", []string{"乌克兰", "烏克蘭", "ukraine"}},
	{"BY", "白俄罗斯", []string{"白俄罗斯", "belarus"}},
	{"LT", "立陶宛", []string{"立陶宛", "lithuania"}},
	{"LV", "拉脱维亚", []string{"拉脱维亚", "latvia"}},
	{"EE", "爱沙尼亚", []string{"爱沙尼亚", "estonia"}},
	{"MD", "摩尔多瓦", []string{"摩尔多瓦", "moldova"}},
	{"RU", "俄罗斯", []string{"俄罗斯", "俄羅斯", "russia", "moscow"}},
	{"TR", "土耳其", []string{"土耳其", "turkey", "istanbul"}},

	// 中东
	{"AE", "阿联酋", []string{"阿联酋", "阿聯酋", "uae", "united arab emirates", "dubai"}},
	{"SA", "沙特", []string{"沙特", "沙烏地", "saudi arabia"}},
	{"IL", "以色列", []string{"以色列", "israel"}},
	{"QA", "卡塔尔", []string{"卡塔尔", "qatar"}},
	{"KW", "科威特", []string{"科威特", "kuwait"}},
	{"OM", "阿曼", []string{"阿曼", "oman"}},
	{"BH", "巴林", []string{"巴林", "bahrain"}},
	{"JO", "约旦", []string{"约旦", "約旦", "jordan"}},
	{"LB", "黎巴嫩", []string{"黎巴嫩", "lebanon"}},
	{"IQ", "伊拉克", []string{"伊拉克", "iraq"}},
	{"IR", "伊朗", []string{"伊朗", "iran"}},

	// 大洋洲
	{"AU", "澳大利亚", []string{"澳大利亚", "澳洲", "australia", "sydney", "melbourne"}},
	{"NZ", "新西兰", []string{"新西兰", "紐西蘭", "new zealand", "auckland"}},

	// 非洲
	{"ZA", "南非", []string{"南非", "south africa", "johannesburg"}},
	{"EG", "埃及", []string{"埃及", "egypt"}},
	{"NG", "尼日利亚", []string{"尼日利亚", "nigeria"}},
	{"KE", "肯尼亚", []string{"肯尼亚", "kenya"}},
	{"MA", "摩洛哥", []string{"摩洛哥", "morocco"}},
	{"DZ", "阿尔及利亚", []string{"阿尔及利亚", "algeria"}},
	{"TN", "突尼斯", []string{"突尼斯", "tunisia"}},

	// 兜底
	{"UN", "未分组", nil},
}

// 启动时根据 regions 构建反查 map，避免每次调用都遍历切片。
var (
	regionCodeToName  = make(map[string]string, len(regions))
	regionAliasToCode = make(map[string]string)
)

func init() {
	for _, r := range regions {
		regionCodeToName[r.Code] = r.Name
		// 注意：主码自身不加入别名表，避免节点名 "direct" 中的 "ir" 之类
		// 二字母片段被 strings.Contains 误判命中。二字母代码识别走
		// containsRegionCodeToken 走严格的单词边界匹配。
		for _, alias := range r.Aliases {
			regionAliasToCode[strings.ToLower(alias)] = r.Code
		}
	}
	// 老的 "UK" 字面量在订阅里很常见，但 ISO 标准是 GB；保留兼容。
	regionAliasToCode["uk"] = "GB"
}
