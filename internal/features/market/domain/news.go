package domain

import "strings"

const (
	CatCapitalMarkets = "capital_markets"
	CatMacro          = "macro"
	CatGlobal         = "global"
	CatRealSector     = "real_sector"
)

var newsCategories = map[string]bool{CatCapitalMarkets: true, CatMacro: true, CatGlobal: true, CatRealSector: true}

func ValidNewsCategory(c string) bool { return newsCategories[c] }

var channelCategory = map[string]string{
	// capital markets: exchange, issuers, banks/financing, funds, crypto, ticker-bound company news
	"market": CatCapitalMarkets, "bursa & saham": CatCapitalMarkets, "emiten": CatCapitalMarkets, "korporasi": CatCapitalMarkets,
	"perbankan": CatCapitalMarkets, "multifinance": CatCapitalMarkets, "asuransi": CatCapitalMarkets, "rekomendasi": CatCapitalMarkets,
	"emas": CatCapitalMarkets, "esg investing": CatCapitalMarkets, "konglomerasi": CatCapitalMarkets, "mymoney": CatCapitalMarkets,
	"personal finance": CatCapitalMarkets, "company": CatCapitalMarkets, "merger": CatCapitalMarkets, "crypto": CatCapitalMarkets,
	"forex": CatCapitalMarkets,
	// macro: economy, policy, national affairs
	"ekonomi": CatMacro, "kurs": CatMacro, "pajak": CatMacro, "moneter": CatMacro, "bisnis": CatMacro, "nasional": CatMacro,
	"hukum": CatMacro, "news": CatMacro, "metro": CatMacro, "info": CatMacro, "rss": CatMacro, "manajemen": CatMacro,
	// global
	"internasional": CatGlobal, "top news": CatGlobal, "business": CatGlobal,
	// real sector: industries and infrastructure
	"energi & tambang": CatRealSector, "energi terbarukan": CatRealSector, "jasa & niaga": CatRealSector, "telekomunikasi": CatRealSector,
	"transportasi & logistik": CatRealSector, "infrastruktur": CatRealSector, "manufaktur": CatRealSector, "properti": CatRealSector,
	"akomodasi": CatRealSector, "mobil": CatRealSector, "sains & teknologi": CatRealSector, "gadget": CatRealSector,
	"travel": CatRealSector, "fashion": CatRealSector, "iklim dan lingkungan": CatRealSector, "health": CatRealSector,
	"entertainment": CatRealSector, "tips bisnis": CatMacro, "tokoh bisnis": CatMacro, "humaniora": CatMacro,
}

var keywordCategory = []struct {
	cat   string
	words []string
}{
	{CatCapitalMarkets, []string{"saham", "ihsg", "emiten", "obligasi", "reksa dana", "kripto", "bitcoin", "dividen", "rights issue", "ipo", "bursa", "stock", "shares", "etf", "crypto"}},
	{CatMacro, []string{"inflasi", "suku bunga", "bi rate", "rupiah", "apbn", "pajak", "ekonomi", "pdb", "fed", "inflation", "gdp"}},
	{CatRealSector, []string{"tambang", "batu bara", "nikel", "migas", "pabrik", "manufaktur", "properti", "konstruksi", "telekomunikasi", "logistik"}},
}

// NewsCategory derives capital_markets|macro|global|real_sector. The channel decides when known;
// ambiguous channels ("all", unknown ones) fall back to title keywords; the last resort is
// global for non-Indonesian articles (region empty or not ID) and macro for Indonesian ones.
func NewsCategory(source, channel, region, title string) string {
	ch := strings.ToLower(strings.TrimSpace(channel))
	if c, ok := channelCategory[ch]; ok {
		return c
	}
	if strings.HasPrefix(ch, "kabar ") || strings.HasPrefix(ch, "bisnis ") || strings.HasPrefix(ch, "bola ") {
		return CatMacro // regional editions and sport: national/regional affairs
	}
	t := strings.ToLower(title)
	for _, k := range keywordCategory {
		for _, w := range k.words {
			if strings.Contains(t, w) {
				return k.cat
			}
		}
	}
	if region == "ID" || (region == "" && !strings.HasPrefix(source, "Finnhub")) {
		return CatMacro
	}
	return CatGlobal
}
