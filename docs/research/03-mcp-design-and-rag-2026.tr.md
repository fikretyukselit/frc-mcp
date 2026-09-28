# 2026 Eylül İtibarıyla Verimli MCP Sunucu Tasarımı ve RAG Mimarisi: Sektör Standardı, TOON, Tool Tasarımı ve Go FRC MCP İçin Somut Plan

Eylül 2026 itibarıyla sektör standardı MCP tasarımı şöyle: **2026-07-28 spec'ine göre stateless Streamable HTTP ile konuşan, az sayıda (yaklaşık 6–12) yüksek seviyeli, salt-okunur ve iyi adlandırılmış tool açan, çıktıyı `outputSchema` + `structuredContent` ile tipli JSON olarak ve kısa Markdown özetiyle veren, büyük kataloglarda ise progressive disclosure / tool search / "search + execute (Code Mode)" desenini kullanan bir sunucu.** TOON varsayılan format olmamalı; yalnızca düzgün, tablo biçimli veride ve ölçerek kullanılmalı. RAG tarafında "en verimli sistem" tek bir teknik değil; **structure-aware chunking + bağlamsal önek + hybrid (BM25 + dense, RRF) + cross-encoder rerank + metadata/sürüm filtresi** yığını ile, bunun yanında ajanın kesin eşleşme (grep benzeri) yapabileceği bir lookup tool'u.

## TL;DR

- **MCP:** Güncel ve final spec **2026-07-28**. Initialize handshake'i ve `Mcp-Session-Id` kaldırıldı, protokol stateless oldu. Tasks resmi bir extension'a taşındı. Roots, Sampling ve Logging ile eski HTTP+SSE transport deprecated.\[1\] Go'da resmi `modelcontextprotocol/go-sdk` (Google Developers Blog'a göre v1.7.0, 28 Temmuz'da spec ile aynı gün yayımlandı ve bu spec'i destekliyor) artık doğru seçim; GitHub ve Grafana da mark3labs/mcp-go'dan buna geçti. Tool sayısını düşük tutun, `search`/`fetch` desenini temel alın, tüm tool'ları `readOnlyHint` ile işaretleyin.
- **TOON:** Resmi benchmark'a göre JSON'dan %42,6 daha az token harcıyor ve doğruluk eşit (%72,2'ye karşı %71,4). Ancak Kutschka ve Geiger'in bağımsız agentic benchmark'ı ("Notation Matters", arXiv 2605.29676v2, 17 Haziran 2026) uçtan uca döngüde yalnızca %18'e kadar tasarruf buldu; bunun bedeli 9 puanlık doğruluk kaybı, çok turlu parse hataları ve çoğu modelde paralel tool çağrılarının bozulması. Sonuç: tool **girdisi ve şeması** için kullanmayın; sadece uzun, düzgün tablo çıktılarında (motor spec listesi, release listesi) opsiyonel bir format olarak sunun ve kendi eval'inizle ölçün.
- **RAG + FRC önerisi:** Postgres (pgvector + full-text/ParadeDB BM25) veya Qdrant üzerinde sezon/sürüm metadata'lı hybrid arama; ardından Cohere Rerank 4 / Voyage rerank-2.5 veya açık kaynak bge/Qwen3 reranker. Vendordep JSON ve spec verileri embedding'e değil yapısal tabloya girmeli. Sunucu yaklaşık 9 tool'dan oluşmalı: `frc_search`, `frc_fetch`, `frc_api_lookup`, `frc_vendordep`, `frc_check_compat`, `frc_releases`, `frc_hardware_specs`, `frc_community`, `frc_scaffold`. İçerik hash'i ile incremental re-index yapılmalı.

---

## BÖLÜM 1 — 2026 İtibarıyla MCP Sunucu Tasarımı

### 1.1 Spec durumu: eskimiş ve güncel bilgi

| Revizyon | Durum (Eylül 2026) | Öne çıkanlar |
|---|---|---|
| 2025-06-18 | Eski; hâlâ geriye dönük destekleniyor | Structured tool output (`outputSchema`/`structuredContent`), elicitation, `resource_link`, OAuth Resource Server ayrımı |
| 2025-11-25 | Bir önceki stabil sürüm; bu sürümle çalışan istemci ve sunucular çalışmaya devam ediyor\[2\] | Deneysel **Tasks** (SEP-1686), URL-mode elicitation (SEP-1036), sampling'de tool calling (SEP-1577), **Client ID Metadata Documents** (SEP-991), tool/resource ikonları (SEP-973), JSON Schema 2020-12 varsayılan (SEP-1613), OIDC Discovery, artımlı scope onayı (SEP-835)\[3\]\[4\] |
| **2026-07-28** | **Güncel, final (28 Temmuz 2026)**\[1\]\[5\] | Stateless çekirdek, `server/discover`, MRTR, header tabanlı yönlendirme, önbelleklenebilir list sonuçları, Extensions çerçevesi (Tasks, MCP Apps, EMA), auth sertleştirme, resmi deprecation politikası\[1\]\[2\] |

**2026-07-28'de değişen ve Go sunucunuzu doğrudan etkileyen noktalar:**

- **Handshake ve session yok.** `initialize`/`initialized` ve `Mcp-Session-Id` kaldırıldı (SEP-2575, SEP-2567). Her istek protokol sürümünü, istemci kimliğini ve yeteneklerini `_meta` içinde taşıyor.\[6\]\[7\] Capability öğrenmek isteyen istemci için opsiyonel `server/discover` RPC'si var. Sonuç olarak her istek, basit bir round-robin load balancer arkasındaki herhangi bir instance'a düşebiliyor.\[8\] Resmi blog'un önerisi: durum gerekiyorsa tool'dan açık bir "handle" üretin ve modelin bunu argüman olarak geri göndermesini sağlayın.\[1\]
- **Header tabanlı yönlendirme.** Streamable HTTP isteklerinde `Mcp-Method` ve `Mcp-Name` header'ları zorunlu (SEP-2243).\[8\] Gateway, rate limiter ve WAF, JSON gövdeyi parse etmeden karar verebiliyor.\[1\]
- **Önbelleklenebilir listeler.** `tools/list`, `prompts/list`, `resources/list` ve `resources/read` yanıtları `ttlMs` ve `cacheScope` taşıyor (SEP-2549); sıralama deterministik. Bu, istemcilerin prompt cache'ini stabil tutar.\[1\] Tool listenizi her istekte yeniden sıralamayın.
- **MRTR (Multi Round-Trip Requests, SEP-2322).** Sunucudan istemciye giden `elicitation/create`, `sampling/createMessage` ve `roots/list` yerine sunucu `resultType: "input_required"` döndürüyor, istemci de orijinal çağrıyı `inputResponses` ile tekrarlıyor.\[1\]
- **Tasks** artık `io.modelcontextprotocol/tasks` extension'ı (SEP-2663): poll tabanlı `tasks/get` ve yeni `tasks/update`.\[2\] 2025-11-25'teki deneysel Tasks API'sini kullananların migrasyon yapması gerekiyor.\[8\] Değişiklik bildirimleri tek bir `subscriptions/listen` akışında toplandı.\[1\]\[6\]
- **Deprecated olanlar:** Roots, Sampling ve Logging (SEP-2577) ile eski HTTP+SSE transport.\[9\] En az 12 ay çalışmaya devam edecekler, ancak yeni implementasyonlar bunları benimsememeli.\[1\]\[2\]
- **Auth:** RFC 9207 `iss` doğrulaması (SEP-2468), issuer'a bağlı client kimlik bilgileri (SEP-2352), CLI/desktop için `application_type` (SEP-837). **Dynamic Client Registration formal olarak deprecated; tercih edilen yol CIMD.** Enterprise-Managed Authorization bir extension olarak stabil.\[1\]\[10\]
- **Ölçek:** Soria Parra ve Delimarsky'nin 28 Temmuz 2026 tarihli resmi blog yazısına göre Tier 1 SDK'larda (TypeScript, Python, Go, C#) ayda yarım milyara yakın indirme var; TypeScript ve Python SDK'ları toplamda 1 milyar indirme eşiğini aştı.

**Yol haritası (22 Ağustos 2026):** Maintainer'lar iki önemli zayıflığı açıkça kabul ediyor. Birincisi, bir `tools/call` sonucu aynı çıktıyı birden fazla biçimde taşıyabiliyor ve sunucu geliştiricisi istemcinin modele hangisini göstereceğini bilemiyor; bu yüzden tek bir sonuç sözleşmesine gidiliyor. İkincisi, yüz tool'lu bir sunucuya bağlanan model, kullanıcı daha soru sormadan tüm yüzeyin bedelini ödüyor ve liste büyüdükçe tool seçimi kötüleşiyor; bu nedenle bir "progressive discovery" çalışması başlatıldı.\[10\] Ayrıca yerel sunucularda stdio üzerinden Streamable HTTP ile transport birleştirme ve DPoP planlanıyor.\[10\] Buradan çıkan tasarım dersi: **bugün `structuredContent` ile birlikte `content` içinde anlamca eşdeğer bir metin döndürün ve tool yüzeyinizi küçük tutun.**

**Transport kararı:**

| Transport | Artılar | Eksiler | Ne zaman |
|---|---|---|---|
| stdio | Sıfır ağ ve auth karmaşası, yerel IDE/CLI entegrasyonu kolay | Tek kullanıcı, dağıtımı zor, ortak cache yok | Yerel geliştirici aracı |
| Streamable HTTP (2026-07-28 stateless) | Yatay ölçek, standart HTTP altyapısı, header ile yönlendirme, OAuth | Auth ve güvenlik yükü sizde | Uzaktan veya paylaşımlı sunucu (FRC takımları için ideal) |
| HTTP+SSE (eski) | — | Deprecated, bir yıllık geçiş süresi var\[1\] | Kullanmayın |

### 1.2 Tool primitive'leri ve metadata

| Özellik | Artılar | Eksiler / dikkat | Öneri |
|---|---|---|---|
| `outputSchema` + `structuredContent` | İstemci doğrulayabilir, kod ile işlenebilir; OpenAI search/fetch için de bir output schema tanımlanmasını öneriyor\[11\] | İstemcinin hangi biçimi modele göstereceği belirsiz (roadmap bunu çözmeye çalışıyor) | Her tool'da kullanın, `content` içine kısa Markdown özet ekleyin |
| Tool annotations (`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`) | İstemci onay UX'i ve güvenlik politikası için sinyal | Sadece ipucu, yetki sınırı değil | FRC sunucusundaki tüm tool'lar `readOnlyHint: true` olmalı |
| Resources ve resource templates | Uygulama kontrollü bağlam, URI ile adreslenebilir, önbelleklenebilir (`ttlMs`) | Birçok istemci resources'ı modele otomatik göstermiyor; model tetikleyemiyor | Vendordep JSON ve spec tablolarını ek olarak resource olarak da açın; asıl erişim tool'larla olsun |
| `resource_link` | Büyük içeriği inline vermek yerine link döndürme, token tasarrufu | İstemci desteği değişken | Arama sonuçlarında tam doküman yerine kullanın |
| Prompts | Kullanıcı tetiklemeli iş akışı şablonları ("swerve drive kur") | Model tarafından otomatik seçilmez | 2–4 prompt yeterli |
| Pagination (cursor) | Büyük listelerde bağlam kontrolü | — | Liste dönen her tool'da `cursor` ve `limit` bulunsun |
| Elicitation (MRTR üzerinden) | Eksik parametreyi kullanıcıdan isteme | İstemci desteği yeni | Kritik değil, opsiyonel |
| Sampling, Roots, Logging | — | **Deprecated (2026-07-28)** | Yeni kodda kullanmayın |
| Tasks extension | Uzun işler için poll | Extension olgunlaşıyor; önceki API kırıldı | Tam yeniden index gibi admin işlemleri dışında gerekmez |

### 1.3 Tool listesi tasarımı: tool bloat, workflow tool'ları ve progressive disclosure

**Sorunun ölçüsü (Anthropic'in kendi rakamları, Kasım 2025):** 58 tool, konuşma başlamadan yaklaşık 55K token tüketiyor. Anthropic içeride optimizasyon öncesinde 134K tokenlık tool tanımı gördüğünü söylüyor.\[12\] Anthropic'in 24 Kasım 2025 tarihli "Introducing advanced tool use" yazısına göre, 50'den fazla MCP tool'lu bir senaryoda (yaklaşık 72K token tool tanımı) Tool Search Tool toplam bağlam yükünü yaklaşık 77K'dan yaklaşık 8,7K tokene indiriyor (%85 azalma, bağlam penceresinin %95'i korunuyor). Anthropic'in iç MCP eval'lerinde doğruluk Opus 4'te %49'dan %74'e, Opus 4.5'te %79,5'ten %88,1'e çıktı.\[13\] "Tool use examples" eklemek, karmaşık parametre işlemede doğruluğu %72'den %90'a yükseltti.\[14\]

**Code execution / Code Mode:** Anthropic'in Kasım 2025 tarihli "Code execution with MCP" yazısında, Google Drive'dan Salesforce'a giden bir iş akışı tool'lar kod API'si olarak sunulunca 150.000 tokenden 2.000 tokene düştü (%98,7).\[15\]\[16\] Cloudflare'in 20 Şubat 2026 tarihli "Code Mode: give agents an entire API in 1,000 tokens" yazısına göre, sunucu tarafı Code Mode MCP'si sadece `search()` ve `execute()` tool'larıyla 2.500'den fazla endpoint'i yaklaşık 1.000 tokene sığdırıyor; Code Mode olmadan eşdeğer bir MCP sunucusu 1,17 milyon token tüketirdi. cloudflare/mcp README'sine göre tool sonuçları varsayılan olarak yaklaşık 6.000 tokenle sınırlandırılıyor.

| Yaklaşım | Artılar | Eksiler | Ne zaman kullanılır |
|---|---|---|---|
| Çok sayıda ince taneli API-wrapper tool | Basit implementasyon, API'yi birebir yansıtır | Context şişmesi, yanlış tool seçimi, çok round-trip, ara sonuçlar bağlamı kirletir | Neredeyse hiçbir zaman (yalnızca <10 endpoint'li küçük API) |
| Az sayıda yüksek seviyeli workflow/intent tool (Anthropic'in "writing tools for agents" önerisi) | Az token, net seçim, sunucu tarafında birleştirme ve filtreleme | Esneklik azalır, tasarım emeği ister | **Varsayılan; FRC için doğru seçim** |
| Toolset gruplama ve filtreleme (GitHub MCP: `--toolsets`, `--read-only`, `X-MCP-Tools`) | Kullanıcı ve istemci ihtiyaca göre yüzeyi küçültür\[17\]\[18\] | Konfigürasyon yükü; filtre bir yetki sınırı değil\[19\] | 20'den fazla tool varsa |
| Dinamik toolset keşfi (GitHub `--dynamic-toolsets`: 4 tool ile başlar) | Başlangıç bağlamı minimal\[20\] | Ekstra tur; istemcinin `list_changed` desteği gerekir | Büyük, çok alanlı sunucular |
| İstemci tarafı tool search (Anthropic `defer_loading`, regex/BM25 veya embedding ile özel `tool_reference`) | Binlerce tool'da doğruluğu korur\[21\]\[22\] | Sunucunun kontrolünde değil; istemciye ve modele bağlı | İstemci sizseniz ya da çok sunuculu ortamda |
| Code Mode / code execution (search + execute) | En büyük token tasarrufu, zincirleme ve filtreleme sandbox'ta yapılır | Sandbox, güvenlik ve izolasyon maliyeti; model kod yazmada hata yapabilir; auth'un yerini tutmaz\[23\] | Çok büyük API yüzeyleri (100'den fazla işlem) |
| OpenAI deep research standardı (`search` + `fetch`, salt-okunur) | ChatGPT deep research ve company knowledge ile uyumlu, tüm istemcilerde anlaşılır\[11\]\[24\] | Tek başına zengin iş akışlarını karşılamaz | **Her doküman ve bilgi sunucusunun çekirdeği olmalı** |

**Tool yazım kuralları (Anthropic "Writing effective tools for agents" ve GitHub/OpenAI rehberlerinden birleştirilmiş):**

1. **Namespacing:** servis/kaynak öneki kullanın (`frc_search`, `frc_vendordep`). Anthropic'in tool search dokümanı da ön ek ile gruplamayı öneriyor; böylece tek bir arama tüm grubu bulur.\[25\]
2. **`response_format` enum'u (`concise` / `detailed`):** Anthropic'in Slack örneğinde concise yanıtlar token'ların yaklaşık ⅓'ünü kullanıyor. ID'ler detailed modda dönüyor.\[26\]
3. **Yüksek sinyalli alanlar:** UUID/teknik ID yerine insan okunur adlar; gereksiz alanları atın.\[27\]
4. **Açıklamayı "yeni ekip arkadaşına anlatır gibi" yazın:** ne zaman kullanılır, ne zaman kullanılmaz, örnek sorgu. Parametre adları belirsiz olmasın (`season` değil `frc_season_year: 2026`).
5. **LLM'in düzeltebileceği hatalar:** go-sdk v1.5.0'dan itibaren input doğrulama hataları JSON-RPC hatası olarak değil, tool sonucu (`isError: true`) olarak dönüyor.\[28\] Mesajda neyin yanlış olduğu ve geçerli değerler yazılmalı ("`library` şunlardan biri olmalı: phoenix6, revlib, …").
6. **Truncation ve pagination:** yanıt başına bir token bütçesi belirleyin (Cloudflare'de yaklaşık 6K). Kesilen çıktıyı açıkça işaretleyin\[29\] ve bir sonraki `cursor`'ı verin.
7. **Eval ile iterasyon:** Anthropic gerçekçi görevlerle eval çalıştırmayı ve doğruluk, çalışma süresi, token ve tool hatası metriklerini izlemeyi öneriyor.\[30\]

### 1.4 "Decision model": model tool'u nasıl seçiyor ve router katmanı

Model, tool seçimini **yalnızca adı, açıklamayı ve şemayı okuyarak** yapıyor. Ayrı bir "decision model" standardı yok; pratikte beş desen kullanılıyor:

| Desen | Artılar | Eksiler | Ne zaman |
|---|---|---|---|
| Doğrudan seçim (tüm tool'lar bağlamda) | En basit, ek gecikme yok | Tool sayısı arttıkça doğruluk düşer (MCP roadmap bunu açıkça kabul ediyor)\[10\] | 15'ten az tool |
| Semantic tool retrieval (embedding/BM25 ile top-k tool) | Binlerce tool'a ölçeklenir | Retrieval hatası doğrudan görev hatasına dönüşür; bakım gerekir | Gateway/istemci katmanı |
| Intent-based tek giriş tool'u (`frc_search`, sunucu içinde yönlendirme) | Model tek karar verir; sunucu kaynak seçimini deterministik yapar | Sunucu içi router'ın kalitesi kritik | **FRC sunucusu: sunucu içi yönlendirme** |
| Planner-executor / advisor | Zor kararlarda güçlü model, geri kalanında ucuz model\[31\] | Maliyet ve gecikme, orkestrasyon karmaşası | Uzun, çok adımlı ajanlar (sunucunun değil istemcinin işi) |
| MCP gateway/proxy (birleşik endpoint, auth, filtre) | Merkezi yönetişim, `Mcp-Method`/`Mcp-Name` ile yönlendirme | Ek bileşen; kendisi hedef haline gelir | Çok sunuculu kurumsal ortam |

**FRC için karar:** Router'ı modelin karşısına tool olarak koymayın, sunucunun içine koyun. `frc_search` sorguyu analiz etsin (sınıf ya da metot adı gibi görünüyorsa exact/BM25 ağırlığını artırsın, "nasıl yapılır" sorusuysa dense ağırlığını artırsın) ve `source`/`library`/`season` filtrelerini uygulasın. Model yalnızca "ara → oku → gerekirse kesin lookup" döngüsünü yürütsün.

### 1.5 TOON (Token-Oriented Object Notation)

**Nedir:** JSON veri modelini kayıpsız biçimde kodlayan, girinti tabanlı bir format.\[32\]\[33\]\[34\] Düzgün (uniform) nesne dizilerini `users[2]{id,name,role}:` başlığı ve CSV benzeri satırlarla yazıyor; alan adlarını tekrar etmiyor, dizi uzunluğunu açıkça belirtiyor.\[35\] Spec v3.0, 24 Kasım 2025 tarihli; medya tipi `text/toon`.\[36\]

**İddialar ve bağımsız doğrulama:**

| Kaynak | Bulgu | Değerlendirme |
|---|---|---|
| toon-format resmi benchmark | TOON %72,2 doğruluk, JSON %71,4; token'da %42,6 daha az. CSV 244 sorudan yalnızca 109'unu destekliyor\[33\] | Üreticinin kendi ölçümü; sadece **okuma/anlama** görevlerinde; veri şekline çok bağlı |
| InfoQ (Kasım 2025) | Yaklaşık %40 tasarruf "bazı durumlarda"; tasarrufun varlığı ve miktarı veri şekline bağlı\[37\] | İkincil kaynak; iddiayı tekrarlıyor ama sınırlarını da belirtiyor |
| arXiv 2603.03306 (Matveev, 2026) — **üretim (generation)** | Plain JSON üretimi en iyi one-shot ve final doğruluğu veriyor; kısa bağlamlarda TOON'un avantajını talimat yükü ("prompt tax") yiyor; basit yapılarda constrained decoding ile JSON üretimi TOON'dan bile az token kullanıyor\[38\] | Bağımsız; TOON'u **çıktı formatı** olarak zayıf buluyor |
| arXiv 2605.29676 "Notation Matters" (Kutschka ve Geiger, v2, 17 Haziran 2026) — **uçtan uca agentic** (BFCL, MCPToolBench++, MCP-Universe, StableToolBench; 5 açık model) | TOON 9 puanlık doğruluk kaybıyla en fazla %18 azalma sağlıyor. Çok turlu parse hataları zincirleniyor ve çoğu modelde paralel tool çağrısı çıktısı çöküyor. Tool şemalarında −%23, tool sonuçlarında −%32'ye kadar sıkışma var. Yazarlara göre TOON "varsayılan olarak güvenli değil"; TRON, JSON baseline'ına 14 puan yakınlıktaki doğrulukla en fazla %27 tasarruf sağlıyor | **En ilgili bağımsız kanıt**; yalnızca açık modellerde test edildi, Claude/GPT sınıfı modellerde sonuç farklı olabilir |

**Ne zaman işe yarar, ne zaman yaramaz:**

| Format | Artılar | Eksiler | Ne zaman |
|---|---|---|---|
| JSON (pretty) | Evrensel, `structuredContent` ile uyumlu, en yüksek üretim doğruluğu | En çok token | Varsayılan `structuredContent` |
| Compact JSON (minified) | Sıfır risk, belirgin tasarruf; resmi benchmark'ta 2.892'ye karşı 4.308 token\[33\] | Okunabilirlik düşük | Metin `content` içinde veri döndürürken |
| TOON | Düzgün tablo dizilerinde büyük tasarruf; uzunluk bildirimi modele sayma kolaylığı sağlar | İç içe ve düzensiz veride avantaj kaybolur; agentic çok turda hata; Go kütüphaneleri topluluk kalitesinde | **Yalnızca** uzun ve düzgün listeler (motor spec tablosu, release listesi), `content` metninde ve opsiyonel |
| CSV | Düz tablo için en küçük format | İç içe yapı yok; tip ve kaçış sorunları | Tamamen düz tablo |
| Markdown tablo / başlıklı metin | Modelin en alışık olduğu biçim; doküman chunk'ları için doğal | Veri olarak parse edilmesi zor | **RAG chunk'ları ve dokümantasyon** |
| YAML | Okunabilir | JSON compact'tan fazla token; girinti hataları | Nadiren |

**Go kütüphaneleri:** `github.com/toon-format/toon-go` resmi organizasyonda ama kendini "community-driven" olarak tanımlıyor. Ayrıca `sstraus/toon_go` (spec v3.0 uyumlu olduğunu iddia ediyor), `jonelmawirat/toon`, `mateuszkardas/toon-go` (v1.3 spec) gibi seçenekler var.\[39\]\[40\]\[41\] Spec sürümleri arasında uyumsuzluk riski bulunuyor; seçtiğiniz kütüphanenin v3.0 conformance testlerini geçtiğini doğrulayın.

**Karar:** TOON'u tool **şemalarında ve girdilerinde** kullanmayın; model TOON üretmek zorunda kalmasın. Çıktıda `format: "json" | "toon"` gibi bir parametreyle, yalnızca 20 satırdan uzun düzgün tablolarda deneyin ve kendi eval setinizde doğruluğu ve token'ı ölçün. Veriler (%18–%42 aralığı, doğruluk çelişkisi) şunu gösteriyor: tasarruf gerçek ama agentic döngüde güvenilirlik bedeli ödeniyor. Asıl büyük kazanç formatta değil, **ne döndürdüğünüzde**: filtreleme, `concise` mod, top-k sınırı ve `resource_link`.

### 1.6 Performans ve verimlilik kontrol listesi

- **Token bütçesi:** tool başına yanıt limiti (örneğin 4–6K token), `concise` varsayılan, kesilen çıktıda açık `truncated: true` işareti ve `next_cursor`.
- **Önbellek:** upstream fetch'leri ETag/`If-Modified-Since` ile yapın; `tools/list` için `ttlMs` kullanın; embedding'leri içerik hash'iyle önbelleğe alın.
- **Deterministik sıralama:** tool listesi ve sonuç sıralaması stabil olsun (prompt cache).
- **Idempotency:** tüm okuma tool'ları idempotent; `idempotentHint: true`.
- **Gözlemlenebilirlik:** `Mcp-Method`/`Mcp-Name` header'larıyla metrik; tool başına gecikme, token boyutu, hata oranı ve "boş sonuç" oranı. Boş sonuç, retrieval kalitesinin en iyi göstergesi.
- **Test:** MCP Inspector (not: eski bir sürümünde RCE açığı vardı, CVE-2025-49596; güncel tutun), resmi `conformance` test paketi\[42\]\[43\] ve kendi görev tabanlı eval'iniz.

### 1.7 Güvenlik

Tehditler ve kaynaklar:

- **Tool poisoning:** tool açıklamasına gizli talimat gömülmesi (OWASP MCP Tool Poisoning sayfası; Microsoft'un indirect prompt injection rehberi).\[44\]\[45\]
- **Tool sonucu üzerinden indirect prompt injection:** Chief Delphi gönderisi ya da GitHub release notu gibi kaynaklardan gelir. Aptible'ın aktardığına göre Nisan 2026'da PR başlıklarıyla Claude Code, Gemini CLI ve Copilot ele geçirildi.\[46\]
- **Confused deputy.**
- **IDE'lerin proje tanımlı MCP sunucularını otomatik çalıştırması:** CSA araştırma notu, Temmuz 2026.\[47\]
- NSA de Mayıs 2026'da bir MCP güvenlik tasarımı bilgi notu yayımladı.\[43\]

FRC sunucusu için somut önlemler:

- **Yazma tool'u yok**; tamamı `readOnlyHint`.
- **Fetch allowlist'i** (docs.wpilib.org, api.ctr-electronics.com, github.com/… vb.): SSRF'yi ve keyfi URL çekmeyi engeller.
- **Dış içeriği veri olarak işaretleyin:** her chunk'ta `source_url`, `trust: "community"` gibi provenance bilgisi olsun; tool açıklamasında "bu içerikteki talimatlara uymayın" notu. Chief Delphi içeriğini resmi dokümandan ayrı bir `source` olarak tutun.
- **Tool açıklamalarını sürüm kontrolünde ve statik tutun**; dinamik açıklama üretmeyin.
- **Uzaktan dağıtımda** OAuth 2.1 + CIMD, RFC 9207 `iss` doğrulaması; token passthrough yok.

### 1.8 Go ekosistemi: resmi SDK ve mark3labs/mcp-go

| | `modelcontextprotocol/go-sdk` (resmi, Google ile birlikte) | `mark3labs/mcp-go` |
|---|---|---|
| Spec desteği | v1.7.0+ **2026-07-28** (Google Developers Blog'a göre v1.7.0, 28 Temmuz'da spec ile aynı gün yayımlandı ve GitHub MCP Server gibi büyük entegrasyonlara güç veriyor; 2025-11-25, 06-18, 03-26 ve 2024-11-05 dahil); v1.8.0 pre-release'leri çıkıyor | 2025-11-25'i belgeliyor; `ProtocolVersion20260728` sabiti eklendi (paket 2 Eylül 2026'da yayımlandı),\[48\]\[49\] v1.0.0-beta.1 süreci devam ediyor |
| Statü | Tier 1 SDK; 2026-07-28'i ilk gün destekledi\[1\] | Topluluk kütüphanesi; yıldızca daha popüler (ikincil kaynağa göre yaklaşık 9k'ya karşı 5k)\[50\] |
| API tarzı | Go struct'larından reflection ile şema (`jsonschema` tag'leri), typed `AddTool[In, Out]` | Builder ile açık şema, daha az boilerplate |
| Üretim sinyali | **GitHub MCP Server** (Aralık 2025) ve **Grafana mcp-grafana** (Eylül 2026 PR'ı, go-sdk v1.8.0-pre.2) resmi SDK'ya geçti\[18\]\[51\] | Mevcut sunucuların çoğu hâlâ bunun üzerinde\[50\] |
| Dikkat | Daha minimal; CORS gibi şeyleri kendiniz eklersiniz (Grafana migrasyonunda görüldü)\[51\] | Spec'i takip etmede gecikme riski |

**Karar:** Yeni bir proje için **resmi go-sdk (≥ v1.7)**. Typed input/output struct'ları doğrudan `outputSchema` üretir; FRC tool'ları için ideal.

### 1.9 Eleştiriler: "MCP vs CLI/code"

- **Token vergisi:** bizzat Anthropic ve Cloudflare, direkt tool çağrısının büyük yüzeylerde ölçeklenmediğini gösterdi; çözüm olarak code execution önerdiler.\[16\]\[52\]
- **Belirsiz sonuç sözleşmesi:** `content` ile `structuredContent` ikiliği; roadmap bunu kabul ediyor.
- **Güvenlik olgunluğu adopsiyonun gerisinde** (NSA, CSA ve OWASP kaynakları).\[43\]\[53\]
- **Spec dalgalanması:** 2025-11-25'teki deneysel Tasks'ı kullananlar 8 ay sonra kırılmaya uğradı.\[2\]\[8\]

Karşı argüman: 2026-07-28 ile uzak MCP sunucusu "herhangi bir HTTP iş yükü" haline geldi.\[10\] Standart auth, keşif ve istemci ekosistemi (Claude, ChatGPT, Copilot, Cursor) bir CLI'da yok. **Sonuç:** Bilgi sunan (docs/RAG) bir sunucu için MCP doğru soyutlama. CLI veya code-mode alternatifleri, çok büyük API yüzeylerinde ve yerel geliştirici ajanlarında daha güçlü.

---

## BÖLÜM 2 — 2026 İtibarıyla En Verimli RAG Sistemi

### 2.1 Retrieval yığını: kanıtlanmış kazanımlar

Anthropic'in Contextual Retrieval çalışmasındaki top-20 retrieval hata oranları:

- Temel: %5,7
- Contextual embeddings: %3,7 (−%35)
- Contextual embeddings + contextual BM25: %2,9 (−%49)
- Bunlara ek olarak reranking: %1,9 (−%67)\[54\]\[55\]

**Eleştiri (dikkate alın):** Bağımsız bir analiz, Anthropic'in kendi tablolarında **sadece BM25 + reranking ile %3,5'e** inildiğini gösteriyor. Ayrıca ArXiv makalelerinde contextual retrieval'ın top-20'de fayda sağlamadığı görülüyor.\[56\] Yani kazancın büyük kısmı hybrid + rerank'ten geliyor; bağlamsal önek domain'e bağlı bir ek iyileştirme.

| Teknik | Artılar | Eksiler | Ne zaman |
|---|---|---|---|
| Sabit boyutlu chunking | Basit | Başlık, kod ve tablo bölünür | Hızlı prototip |
| **Structure-aware chunking** (başlık, bölüm, kod bloğu, API sembolü sınırları) | Anlamsal bütünlük, doğal citation | Parser yazmak gerekir | **Teknik doküman ve API referansı (FRC)** |
| Semantic chunking (embedding benzerliğiyle bölme) | Yapısız metinde iyi | Pahalı, kararsız sınırlar | Yapısız uzun metin |
| Late chunking (uzun bağlamlı embedding'den sonra bölme) | Chunk'a belge bağlamı taşır, LLM çağrısı yok | Uzun bağlam destekli embedding modeli gerekir | Orta ölçek, maliyet hassasiyeti varsa |
| **Contextual prefix** (LLM ile veya deterministik breadcrumb) | −%35'e varan hata azalması (domain'e bağlı) | LLM ile üretilirse index maliyeti (prompt caching ile düşer) | Breadcrumb her zaman; LLM ile üretilen prefix eval gösterirse |
| **Hybrid BM25 + dense (RRF)** | Sembol ve sürüm adlarında BM25, kavramda dense; Qdrant'ın ölçümüne göre varsayılan RRF beş veri setinin dördünde en iyi tekil retriever'ı geçti\[57\]\[58\] | İki index | **Her zaman** |
| **Cross-encoder rerank** | En büyük tekil kazanç; bağlama giren token'ı azaltır | +100–600 ms gecikme, API maliyeti | Top-50 → top-5/8 için her zaman |
| ColBERT / late interaction | Rerank'e yakın kalite, daha hızlı | Depolama ağır; Go ekosisteminde zayıf | Büyük ölçek |
| Query rewriting / HyDE | Belirsiz sorgularda recall artışı | Ek LLM çağrısı; agentic istemci zaten sorguyu yeniden yazıyor | Sunucuda genelde gereksiz |
| Parent-document / small-to-big | Küçük chunk ile bul, büyük bölümü döndür | Token artışı | `frc_fetch` ile birleşik kullanım |
| Multi-vector (başlık, özet, kod ayrı vektörler) | Farklı sorgu tiplerine uyum | Index boyutu | API referansında sembol ve açıklama ayrı |

### 2.2 Embedding ve reranker seçimi (2026)

**Embedding:** MTEB v2, v1 ile karşılaştırılabilir değil; sıralamalar hızla değişiyor.

- Qwen3-Embedding-8B, Haziran 2025'te MMTEB'de 70,58 ile birinciydi; açık ağırlıklı ve ticari kullanıma uygun lisanslı ailesi (0,6B/4B/8B) 2026'da da referans. Google gemini-embedding-001, MTEB(Code) dahil birçok tabloda üst sırada.\[59\]\[60\]
- İkincil kaynaklara göre KaLM-Embedding-Gemma3-12B, Mayıs 2026 MMTEB v2 anlık görüntüsünde retrieval'da 75,7 ile önde; Microsoft Harrier-oss-v1-0.6B (MIT, Nisan 2026) küçük modellerde güçlü görünüyor.\[60\] Bu iki iddiayı birincil kaynaktan doğrulamadım.
- Pratik ders: bir hukuk RAG vakasında MTEB'in ilk 3'ü şirketin kendi eval'inde 5., 7. ve 2. sıraya düştü.\[61\] **Liderlik tablosu sadece bir ön eleme aracıdır; kendi FRC soru setinizde ölçün.**

**Reranker (Agentset bağımsız ELO tablosu, 15 Şubat 2026):** Zerank-2 1638, Cohere Rerank 4 Pro 1629, Voyage rerank-2.5 1544 (4.). Zerank-2 ağırlıkları CC-BY-NC lisanslı; ticari self-host için uygun değil.\[62\] Açık ve self-host edilebilir seçenekler: Qwen3-Reranker (Apache 2.0), bge-reranker-v2-m3, jina-reranker-v3, mxbai-rerank-v2.\[63\] Aynı Agentset tablosunda (GitHub agentset-ai/reranker-eval) Qwen3 Reranker 8B 1473 ELO ile 8., BAAI/BGE Reranker v2 M3 1327 ELO ile 11. sırada; yani açık modeller API liderlerinin belirgin şekilde gerisinde.

| Seçenek | Artılar | Eksiler | Ne zaman |
|---|---|---|---|
| API embedding (Voyage/Gemini/OpenAI) + API rerank (Cohere 4 / Voyage 2.5) | Sıfır altyapı, üst düzey kalite | Maliyet, ağ gecikmesi, dışa veri gönderimi (FRC dokümanları zaten açık) | Küçük ekip, hızlı başlangıç |
| Self-host Qwen3-Embedding-0.6B + bge/Qwen3 reranker (ONNX/TEI) | Sabit maliyet, offline | GPU/CPU yönetimi, Go'da çıkarım zor (ayrı bir servis gerekir) | Yüksek hacim veya offline turnuva ortamı |

### 2.3 Agentic RAG, long context ve "RAG öldü mü?"

- **Agentic search (grep/glob):** Claude Code'un yaratıcısı Boris Cherny'nin 1 Şubat 2026 tarihli X gönderisi (x.com/bcherny/status/2017824286489383315): ilk sürümler RAG ve yerel vektör DB kullanıyordu, ancak ekip agentic search'ün genel olarak daha iyi çalıştığını, daha basit olduğunu ve güvenlik, gizlilik, bayatlık ve güvenilirlik sorunlarını taşımadığını gördü. **Bağlam önemli:** bu bulgu kod tabanı keşfi için geçerli. Tam eşleşme orada kritik ve dosyalar sürekli değişiyor.\[64\]\[65\] Geniş, doğal dilde sorgulanan bir doküman korpusunda (WPILib docs ile vendor dokümanlarının birleşimi) semantic retrieval hâlâ değer katıyor.\[66\]
- **Long context:** Her şeyi bağlama yığmak maliyet ve gecikme yaratır, dikkati de dağıtır. Konu artık "context engineering": doğru küçük kümeyi seçmek.
- **Sentez:** En iyi pratik **hybrid**. Semantic ön filtre sağlayan bir `search` tool'u, kesin sembol ve sürüm eşleşmesi için grep benzeri bir `lookup` tool'u ve ajanın yinelemeli olarak her ikisini kullanması.

| Yaklaşım | Artılar | Eksiler | Ne zaman |
|---|---|---|---|
| Klasik tek atış RAG | Hızlı, ucuz, tahmin edilebilir | Çok adımlı sorularda zayıf | Basit soru-cevap |
| Agentic RAG (model birden çok arama yapar) | Karmaşık sorularda güçlü | Daha fazla tur ve token | **MCP istemcisi zaten bunu yapıyor; sunucu iyi primitive'ler vermeli** |
| Agentic grep/glob | Tam eşleşme, index yok, taze | Eşanlamlı ve kavramsal sorularda kaçırır; ham dosya erişimi gerekir | Kod tabanı; FRC'de API sembolü lookup'ı |
| GraphRAG (Microsoft) | Küresel ve tema sorularında güçlü | Pahalı index ve güncelleme: LightRAG makalesine göre sorgu başına yaklaşık 610.000 token ve yüzlerce API çağrısı; artımlı güncellemede 1.399 topluluk × 2 × 5.000 token\[67\] | Nadiren değişen, ilişki yoğun korpus |
| LightRAG | Sorgu başına 100'den az token ve tek API çağrısı (yazarların analitik tahmini); artımlı güncelleme ucuz\[67\] | Graph çıkarma maliyeti yine var; kalite domain'e bağlı | Entity ilişkileri önemliyse (motor → controller → vendordep) |
| ColPali (görsel doküman retrieval) | PDF, şema ve diyagramlarda OCR'sız | Depolama ağır, Go desteği zayıf | Datasheet PDF'leri ağırlıktaysa (ileride) |

**FRC için:** GraphRAG gereksiz. Motor ↔ controller ↔ vendordep ilişkisi küçük ve deterministik; bunu **yapısal bir tabloda** (Postgres) tutun, LLM ile graph çıkarmayın.

### 2.4 Vektör DB seçenekleri (Go kullanılabilirliği)

| Seçenek | Hybrid native mi? | Go client | Artılar | Eksiler | Ne zaman |
|---|---|---|---|---|---|
| **Postgres + pgvector** (0.8.2 Şubat 2026'da yayımlandı, CVE-2026-3172 düzeltmesi içeriyor) + `tsvector` veya **ParadeDB pg_search** (Tantivy tabanlı gerçek BM25)\[68\]\[69\] | RRF'yi SQL'de siz yazarsınız; stok FTS gerçek BM25 değil, pg_search öyle\[70\]\[71\] | pgx (vendor client'ı gerekmez) | Metadata, sürüm, hash, vendordep tabloları ve vektörler tek DB'de; transaction'lı incremental index | Tuning gerekir; pg_search ek extension | **Önerilen: FRC sunucusu için tek veri katmanı** |
| **Qdrant** (server 1.19.x; resmi `qdrant/go-client` v1.19.x, gRPC) | **Evet**: sparse + dense prefetch, `rrf`/DBSF füzyonu, sunucu tarafında IDF\[58\]\[72\]\[73\] | Resmi | Native hybrid, filtreleme, quantization (TurboQuant)\[74\] | Ayrı servis; metadata için yine bir DB gerekir | Ölçek veya native hybrid isteniyorsa |
| Weaviate (Go client v5.7.3, v6 beta)\[75\]\[76\] | Evet (BM25F + vektör, varsayılan relativeScoreFusion)\[77\]\[78\] | Resmi | Olgun hybrid | Ağır; açık bir hybrid merge bug raporu var\[79\] | Kurumsal |
| Milvus 2.5+ (Go client v2) | Evet (yerleşik BM25 + `HybridSearch`, RRF)\[80\]\[81\] | Resmi | Çok büyük ölçek | Operasyonel ağırlık; Milvus Lite'ta full-text yok\[80\] | Yüz milyonlarca vektör (FRC için fazla) |
| sqlite-vec (stabil v0.1.9; ANN yalnızca alpha) + FTS5\[82\]\[83\] | SQL ile manuel RRF\[84\] | CGO (`mattn/go-sqlite3`) veya WASM (`ncruces`)\[85\] | Tek dosya, gömülü, offline | Stabil sürümde brute force; yazarın ifadesiyle "1 milyon vektörde sınırlar belirginleşiyor"\[86\] | Offline/laptop dağıtımı |
| chromem-go | Hayır (yalnızca vektör) | Saf Go, sıfır bağımlılık | CGO yok; 100k dokümanda yaklaşık 40 ms (yazarın ölçümü)\[87\] | Beta; BM25 yok; bellek içi\[87\] | Tek binary prototip (BM25 için Bleve ile birlikte) |
| LanceDB | FTS indeksi var | Yalnızca topluluk Go SDK'sı (CGO)\[88\]\[89\] | Hızlı IVF-PQ | Go resmi olarak desteklenmiyor\[89\] | Önerilmez (Go için) |

Not: Tek bir kişinin blog benchmark'ında (Nisan 2026; 100k × 1024 boyut, M2 Pro, script'ler yayımlanmamış) p50 gecikmeler şöyle: chromem-go 38 ms, LanceDB-go 6 ms, sqlite-vec brute force 1,7 s.\[90\] Bu rakamlar doğrulanmamıştır, ancak sıralama mantıklı görünüyor.

**FRC ölçeği:** Tahminen 10⁴–10⁵ chunk. Bu ölçekte her seçenek yeterince hızlı; seçimi **operasyon sadeliği ve metadata ihtiyacı** belirlemeli. Bu yüzden Postgres.

### 2.5 Değerlendirme, tazelik ve sürümleme

- **Retrieval metrikleri:** recall@k, nDCG@10, MRR. Altın set: 100–200 gerçek FRC sorusu. Örnekler: "Phoenix 6'da TalonFX Motion Magic nasıl ayarlanır, 2026", "REVLib 2026'da SparkMax config API'si nasıl değişti", "YAGSL swerve için gereken vendordep'ler".
- **Üretim metrikleri:** RAGAS faithfulness, answer relevancy, context precision. Kalibrasyon için küçük bir insan kontrollü set tutun.
- **Tazelik:** kaynak başına ETag ve `Last-Modified` bilgisi; içerik hash'i değişmeyen sayfayı yeniden embed etmeyin. GitHub releases için `since` etiketi, RSS için `guid` kullanın.
- **Sürümleme:** her chunk'ta `season` (2026, 2027-beta), `library`, `library_version`, `doc_version` ("stable"/"latest") ve `valid_from`/`valid_to` alanları. Varsayılan filtre aktif sezon olsun; eski sürümler silinmesin, `deprecated: true` ile işaretlensin (takımlar geçen sezonun kodunu okurken ihtiyaç duyar).

### 2.6 RAG'i MCP üzerinden sunmak

| Karar | Öneri | Gerekçe |
|---|---|---|
| Tools mu resources mı? | Birincil olarak **tools** (`search`/`fetch`); ek olarak resource template'leri | Model tool'ları kendisi tetikleyebiliyor; resources'ın istemci desteği değişken |
| Sonuç formatı | `structuredContent`: `{results:[{id,title,url,season,library,snippet,score}]}`; `content`: numaralı Markdown liste | OpenAI deep research uyumu; model için okunabilirlik |
| Chunk metni | **Markdown** (kod blokları korunmuş) | Doküman için doğal format; TOON'un burada avantajı yok |
| Citation | Her sonuçta kanonik `url`, başlık yolu ve sürüm; `fetch` tam bölümü aynı URL ile döndürsün | Doğrulanabilirlik ve prompt injection kaynağının izlenebilirliği |
| Top-k | Varsayılan 5–8 (rerank sonrası), en fazla 20 | Token bütçesi |

---

## SON BÖLÜM — Go FRC Robot Builder MCP İçin Somut Öneri

### Mimari

```
[Fetchers (cron/worker)]  WPILib docs · Phoenix6/REVLib/YAGSL/PathPlanner/PhotonVision docs+Javadoc
        │                 vendordep JSON · GitHub releases · Chief Delphi RSS · motor/sensör spec
        ▼  (ETag/If-Modified-Since, içerik hash, allowlist)
[Normalizer]  HTML/RST → Markdown, başlık ağacı, kod blokları, API sembolleri
        ▼
[Indexer]  structure-aware chunk + breadcrumb prefix → embedding (API veya ayrı servis)
        ▼
[Postgres]  chunks(tsvector/pg_search BM25 + pgvector) · vendordeps · releases · hardware_specs
        ▲
[MCP Server (Go, modelcontextprotocol/go-sdk ≥ v1.7, 2026-07-28, stateless Streamable HTTP + stdio)]
   iç router: sorgu tipi → BM25/dense ağırlığı, filtreler → RRF → rerank (API veya servis)
```

### Tool listesi taslağı (9 tool; tamamı `readOnlyHint: true`, `idempotentHint: true`)

| # | Tool | Amaç | Girdi (özet) | Çıktı |
|---|---|---|---|---|
| 1 | `frc_search` | Tüm doküman ve topluluk içeriğinde hybrid arama (OpenAI `search` uyumlu) | `query`, `season?`, `library?`, `source?` (docs/javadoc/releases/community), `limit≤20`, `response_format` | id, başlık, url, sürüm, snippet, skor |
| 2 | `frc_fetch` | ID ile tam bölüm (parent-document) (OpenAI `fetch` uyumlu) | `id`, `max_tokens?` | Markdown + citation |
| 3 | `frc_api_lookup` | Kesin sembol lookup'ı (sınıf/metot, Javadoc), grep benzeri | `symbol`, `library`, `season`, `language` (java/cpp/python) | imza, açıklama, url, "since" sürümü |
| 4 | `frc_vendordep` | Vendordep JSON'unu çözümlenmiş halde verir (versiyon, maven URL, jsonUrl, uyumlu WPILib) | `library`, `season`, `version?` | yapısal JSON (embedding yok) |
| 5 | `frc_check_compat` | Vendordep seti ile WPILib sürüm uyumluluğunu doğrular | `wpilib_version`, `vendordeps[]` | uyumlu/uyumsuz + düzeltme önerisi |
| 6 | `frc_releases` | Kütüphane changelog'u ve kırıcı değişiklikler | `library`, `since_version?`, `limit` | liste (uzunsa TOON opsiyonu) |
| 7 | `frc_hardware_specs` | Motor ve sensör spec'leri; karşılaştırma | `parts[]` veya `category`, `fields?` | tablo (TOON opsiyonu burada anlamlı) |
| 8 | `frc_community` | Chief Delphi gönderileri (işaretli, güvenilmeyen kaynak) | `query` veya `topic`, `since?` | başlık, url, tarih, özet, `trust: "community"` |
| 9 | `frc_scaffold` | Workflow tool'u: "şu donanımla subsystem iskeleti" için gerekli vendordep'leri, API'leri ve doküman linklerini tek pakette toplar | `mechanism` (swerve/elevator/arm…), `hardware[]`, `language`, `season` | adımlar, gerekli vendordep'ler, ilgili doc id'leri |

Ek olarak:

- **Resource templates:** `frc://{season}/vendordeps/{library}.json`, `frc://{season}/specs/{part}` (`ttlMs` ile).
- **2–3 prompt:** `build_swerve_drive`, `migrate_season`, `debug_can_bus`.

Bu tasarımda tool sayısı 9. İstemci tarafında tool search gerektirmeyecek kadar küçük. İleride 15'i geçerseniz GitHub tarzı toolset'lere (`docs`, `hardware`, `community`) bölün.

### Output formatı ve TOON kararı

- **Varsayılan:** `outputSchema` ile tipli `structuredContent`, `content` içinde de kısa Markdown. Go'da `mcp.AddTool` ile typed Output struct'ları kullanın.
- **TOON:** sadece `frc_releases` ve `frc_hardware_specs` tool'larında, `format: "toon"` açıkça istenirse ve satır sayısı 20'yi geçerse, `content` metninde. Tool girdisinde ve şemada asla kullanmayın. Eval'de doğruluk düşmüyorsa varsayılan yapmayı düşünün.
- **`response_format: concise|detailed`**, varsayılan `concise`.

### RAG katmanı ayarları

| Bileşen | Öneri | Alternatif |
|---|---|---|
| Chunking | Başlık tabanlı (H2/H3), 300–800 token; kod blokları ve tablolar bölünmez; Javadoc'ta sınıf başına 1 ve metot başına 1 chunk | Late chunking (embedding modeli destekliyorsa) |
| Bağlam öneki | Deterministik breadcrumb: `"[WPILib 2026] Command-Based > Subsystems > …"`; LLM ile üretilen contextual prefix yalnızca eval'de ≥3 puan recall kazancı gösterirse | Claude ile contextual retrieval + prompt caching |
| Embedding | API: Voyage veya Gemini embedding; self-host: Qwen3-Embedding-0.6B (TEI/ONNX servisi) | Kendi eval'inizde en iyi 2–3 aday |
| Arama | BM25 (pg_search veya tsvector) + pgvector HNSW, her biri top-50, RRF (k=60)\[91\] | Qdrant native RRF |
| Rerank | Cohere Rerank 4 veya Voyage rerank-2.5 (API), top-50 → top-8 | bge-reranker-v2-m3 veya Qwen3-Reranker (self-host) |
| Filtre | Varsayılan `season = aktif`, `library` otomatik tespit edilir (sorguda "TalonFX" varsa `phoenix6`) | — |
| Sezon ve sürüm | Chunk metadata'sı: season, library, library_version, doc_version, valid_from/to; eski sezon `deprecated`, silinmez | Sezon başına ayrı partition |
| Incremental re-index | Kaynak başına ETag/Last-Modified; normalize edilmiş içeriğin SHA-256'sı; sadece değişen chunk'ları yeniden embed edin; silinenleri soft-delete; embedding model sürümünü kolon olarak tutun (model değişirse arka planda tam yeniden index) | — |
| Fetch sıklığı | Docs günlük; GitHub releases ve vendordep JSON saatlik (kickoff ve sezon başında daha sık); RSS 15–30 dakikada bir | — |
| Eval | 150 soruluk altın set; recall@10 ≥ 0,9 ve nDCG@10 hedefi; her index veya model değişikliğinde CI'da çalıştırın | RAGAS faithfulness (örneklem üzerinde) |

### Artı ve eksi özeti

| Karar | Artı | Eksi |
|---|---|---|
| 9 intent tool + search/fetch | Az token, net seçim, ChatGPT/Claude/Copilot uyumu | Sunucu içi router'ın kalitesine bağımlılık |
| Postgres tek veri katmanı | Operasyon sadeliği, transaction'lı incremental index, yapısal ve vektör verisi bir arada | Native hybrid füzyon yok (SQL ile RRF), BM25 için extension |
| Hybrid + rerank | Kanıtlanmış en büyük retrieval kazancı | Rerank gecikmesi ve API maliyeti |
| Vendordep ve spec verisi yapısal | Halüsinasyonsuz kesin yanıt | Şema bakımı |
| TOON opsiyonel | Uzun tablolarda token tasarrufu | Ek kod yolu, doğruluk riski |
| 2026-07-28 stateless | Yatay ölçek, basit dağıtım | Session'a dayanan eski istemcilerle uyum testi gerekir (SDK eski sürümleri de destekliyor) |

### Kaçınılacak hatalar

1. **Her vendor API'sini ayrı tool yapmak** (`get_talonfx_config`, `get_sparkmax_config`…); tool bloat'a yol açar.
2. **Vendordep JSON'unu veya motor spec'lerini embed edip RAG'den okutmak**; kesin veriyi yapısal tabloda tutun.
3. **Sezon filtresi olmadan index'lemek**; 2025 ve 2026 API'leri karışır, en sık görülen FRC hatası budur (örneğin REVLib ve Phoenix 6'daki sezonlar arası API değişiklikleri).
4. **Sampling, roots, logging veya HTTP+SSE üzerine inşa etmek** (deprecated).\[1\]
5. **Tool listesini her istekte değiştirmek veya sıralamayı karıştırmak**; prompt cache bozulur.
6. **Chief Delphi içeriğini resmi dokümanla aynı güven seviyesinde sunmak**; prompt injection yolu açılır.
7. **Keyfi URL fetch eden bir tool açmak**; SSRF riski doğar. Yalnızca allowlist kullanın.
8. **Yanıtları kesmeden 20K+ token döndürmek**; limit koyun ve `cursor` verin.
9. **MTEB sırasına göre model seçip kendi eval'inizi yapmamak.**
10. **TOON'u tool girdisi veya şeması olarak kullanmak.**

---

## Caveats

- **Kaynak kalitesi:** Spec değişiklikleri resmi MCP blog'undan ve changelog'dan alındı (birincil kaynak). Anthropic ve Cloudflare'in token rakamları üreticilerin kendi ölçümleri; bağımsız tekrarlar (örneğin 112 GitHub tool'unda yaklaşık %99 azalma) ikincil kaynaklardan geliyor. TOON'un %42,6 iddiası da üreticinin kendi benchmark'ı; bağımsız iki arXiv çalışması üretim ve agentic senaryolarda daha zayıf sonuçlar veriyor ve bunlar yalnızca açık ağırlıklı modellerde test edildi.
- **Doğrulanmamış veya ikincil iddialar:** KaLM-Embedding-Gemma3-12B ve Harrier skorları; mcp-go ile go-sdk yıldız sayıları; pgvector 0.8.4 sürümü (tek bir ikincil kaynakta geçiyor); Go vektör DB gecikme benchmark'ı (tek kişilik blog); LightRAG ile GraphRAG maliyet karşılaştırması (yazarların analitik tahmini, gerçek fatura değil).
- **Türkçe kaynaklar:** Araştırma bütçesi içinde bu konulara (2026 MCP spec'i, TOON, agentic RAG) dair hakemli ya da birincil Türkçe kaynak bulunamadı; Türkçe içeriğin büyük kısmı İngilizce kaynakların özetleri niteliğinde. Bu rapor birincil İngilizce kaynaklara dayanıyor.
- **Hızla değişen alan:** MCP roadmap'i "progressive discovery" ve tek tool sonuç sözleşmesi üzerinde çalışıyor. Önümüzdeki spec revizyonu `content`/`structuredContent` davranışını ve büyük tool kataloglarının sunumunu değiştirebilir; tasarımınızı bu iki noktada esnek tutun.

## Sources

1. [The 2026-07-28 Specification](https://blog.modelcontextprotocol.io/posts/2026-07-28/)
2. [MCP 2026-07-28 spec: every breaking change, with fixes · Stacktree](https://stacktr.ee/blog/mcp-2026-spec-changes)
3. [Key Changes - Model Context Protocol](https://modelcontextprotocol.io/specification/2025-11-25/changelog)
4. [Key Changes – Model Context Protocol （MCP）](https://modelcontextprotocol.info/specification/2025-11-25/changelog/)
5. [Releases · modelcontextprotocol/modelcontextprotocol](https://github.com/modelcontextprotocol/modelcontextprotocol/releases)
6. [MCP specification 2026-07-28 stateless revision — track for future MCP-module compatibility · Issue #298 · bug-ops/mcpls](https://github.com/bug-ops/mcpls/issues/298)
7. [The biggest MCP spec update ships July 28: What changes for AI agent authentication — WorkOS](https://workos.com/blog/mcp-2026-spec-agent-authentication)
8. [The 2026-07-28 MCP Specification Release Candidate | Model Context Protocol Blog](https://blog.modelcontextprotocol.io/posts/2026-07-28-release-candidate/)
9. [Model Context Protocol](https://en.wikipedia.org/wiki/Model_Context_Protocol)
10. [The New MCP Roadmap](https://blog.modelcontextprotocol.io/posts/mcp-roadmap/)
11. [Building MCP servers for plugins and API integrations | OpenAI API](https://developers.openai.com/api/docs/mcp)
12. [MCP Context Bloat Fix 2026 (Tool Search) — MCP.Directory](https://mcp.directory/blog/mcp-context-bloat-fix-2026-tool-search-code-mode-progressive-disclosure)
13. [Introducing advanced tool use on the Claude Developer ...](https://www.anthropic.com/engineering/advanced-tool-use?939688b5_page=2&ss_ad_code=m_FB)
14. [AI Agent Tool Sprawl: How Anthropic's 2026 Upgrades Fix It - DEV Community](https://dev.to/abhijeet_singh_4577af3ef9/ai-agent-tool-sprawl-how-anthropics-2026-upgrades-fix-it-3ccd)
15. [Code Execution With MCP: Cut Tool Tokens up to 98%](https://particula.tech/blog/code-execution-mcp-token-reduction-pattern)
16. [Engineering at Anthropic](https://www.anthropic.com/engineering/code-execution-with-mcp)
17. [GitHub - github/github-mcp-server: GitHub's official MCP Server · GitHub](https://github.com/github/github-mcp-server)
18. [The GitHub MCP Server adds support for tool-specific configuration, and more - GitHub Changelog](https://github.blog/changelog/2025-12-10-the-github-mcp-server-adds-support-for-tool-specific-configuration-and-more/)
19. [github-mcp-server/docs/server-configuration.md at main · github/github-mcp-server](https://github.com/github/github-mcp-server/blob/main/docs/server-configuration.md)
20. [Feedback for dynamic tool selection 🚀 · Issue #275 · github/github-mcp-server](https://github.com/github/github-mcp-server/issues/275)
21. [Tool Search | liteLLM](https://docs.litellm.ai/docs/providers/anthropic_tool_search)
22. [Anthropic Claude tool use - Amazon Bedrock](https://docs.aws.amazon.com/bedrock/latest/userguide/model-parameters-anthropic-claude-messages-tool-use.html)
23. [Code Mode MCP server patterns · Cloudflare Agents docs](https://developers.cloudflare.com/agents/model-context-protocol/codemode/)
24. [ChatGPT 🤝 FastMCP - FastMCP](https://gofastmcp.com/integrations/chatgpt)
25. [Tool search tool - Claude Platform Docs](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool)
26. [Writing Effective Tools for Agents: Complete MCP Development Guide](https://modelcontextprotocol.info/docs/tutorials/writing-effective-tools/)
27. [Writing Effective Tools for AI Agents: Lessons from Anthropic | by LaxmiKumar Reddy Sammeta | Medium](https://laxmikumars.medium.com/writing-effective-tools-for-ai-agents-lessons-from-anthropic-25b85bf74f5d)
28. [\[go-fan\] Go Module Review: modelcontextprotocol/go-sdk · github/gh-aw · Discussion #28874](https://github.com/github/gh-aw/discussions/28874)
29. [Build a search and execute MCP server · Cloudflare Agents docs](https://developers.cloudflare.com/agents/model-context-protocol/guides/build-codemode-openapi-mcp-server/)
30. [Agent Tools: Building Effective Capabilities for AI Systems](https://www.firecrawl.dev/blog/agent-tools)
31. [Anthropic’s Advanced Tool Use Platform: Programmatic Calling, Advisor Strategy, and the Future of Claude Agents | The Agent Report](https://the-agent-report.com/2026/06/anthropic-advanced-tool-use-platform-june-2026/)
32. [toon package - github.com/jonelmawirat/toon - Go Packages](https://pkg.go.dev/github.com/jonelmawirat/toon)
33. [GitHub - toon-format/toon: 🎒 Token-Oriented Object Notation (TOON) – compact, human-readable serialization of JSON data for LLM prompts. TypeScript SDK, CLI, benchmarks.](https://github.com/toon-format/toon)
34. [TOON Format: The 40% Token Savings That Still Can’t Dethrone JSON | by Tasmay Shah | Medium](https://tasmayshah12.medium.com/toon-format-the-40-token-savings-that-still-cant-dethrone-json-b220a9dd4eaa)
35. [toon package - github.com/wilchen558/go-toon - Go Packages](https://pkg.go.dev/github.com/wilchen558/go-toon)
36. [Token-Oriented Object Notation](https://en.wikipedia.org/wiki/Token-Oriented_Object_Notation)
37. [New Token-Oriented Object Notation (TOON) Hopes to Cut LLM Costs by Reducing Token Consumption - InfoQ](https://www.infoq.com/news/2025/11/toon-reduce-llm-cost-tokens/)
38. [arxiv.org](https://arxiv.org/pdf/2603.03306)
39. [GitHub - toon-format/toon-go: 🐹 Community-driven Go implementation of TOON](https://github.com/toon-format/toon-go)
40. [toon package - github.com/mateuszkardas/toon-go - Go Packages](https://pkg.go.dev/github.com/mateuszkardas/toon-go)
41. [GitHub - sstraus/toon\_go: Token-Oriented Object Notation for Go with encoding / decoding · GitHub](https://github.com/sstraus/toon_go)
42. [Model Context Protocol · GitHub](https://github.com/modelcontextprotocol)
43. [Model Context Protocol (MCP): Security Design ...](https://media.defense.gov/2026/Jun/02/2003943289/-1/-1/0/CSI_MCP_SECURITY.PDF)
44. [MCP Tool Poisoning | OWASP Foundation](https://owasp.org/www-community/attacks/MCP_Tool_Poisoning)
45. [Protecting against indirect prompt injection attacks in MCP - Microsoft for Developers](https://developer.microsoft.com/blog/protecting-against-indirect-injection-attacks-mcp/)
46. [Prompt Injection in MCP: Tool Poisoning and Blast Radius | Aptible](https://www.aptible.com/mcp-security/mcp-prompt-injection)
47. [MCP Attack Surface: Tool Poisoning and IDE Auto-Execution – Lab Space](https://labs.cloudsecurityalliance.org/research/csa-research-note-mcp-tool-poisoning-auto-execution-20260701/)
48. [mcp package - github.com/mark3labs/mcp-go/mcp - Go Packages](https://pkg.go.dev/github.com/mark3labs/mcp-go/mcp)
49. [GitHub - mark3labs/mcp-go: A Go implementation of the Model Context Protocol (MCP), enabling seamless integration between LLM applications and external data sources and tools. · GitHub](https://github.com/mark3labs/mcp-go)
50. [MCP Server Frameworks & SDKs — FastMCP, Official SDKs, and the Tools That Power Every MCP Server — ChatForest](https://chatforest.com/reviews/mcp-server-frameworks-sdks/)
51. [feat: migrate from mark3labs/mcp-go to official go-sdk by sd2k · Pull Request #1180 · grafana/mcp-grafana](https://github.com/grafana/mcp-grafana/pull/1180)
52. [Code Mode: give agents an entire API in 1,000 tokens | Cloudflare Blog](https://blog.cloudflare.com/code-mode-mcp/)
53. [MCP Security: Top 7 Risks and Critical Best PracticesMCP Security Best Practices: The Complete 2026 Guide](https://obot.ai/resources/learning-center/mcp-security/)
54. [Building Production RAG with Anthropic’s Contextual Retrieval: Complete Python Implementation | by Reliable Data Engineering | Medium](https://medium.com/@reliabledataengineering/building-production-rag-with-anthropics-contextual-retrieval-complete-python-implementation-f8a436095860)
55. [Is RAG Dead Anthropic Says No](https://cloudurable.com/blog/is-rag-dead-anthropic-says-no/)
56. [Responding to Anthropic’s Contextual Retrieval for RAG Apps… Why Context is NOT All you Need](https://medium.com/almond-ai/responding-to-anthropics-contextual-retrieval-for-rag-apps-why-context-is-not-all-you-need-af503985aa55)
57. [Hybrid Search in Qdrant - Qdrant](https://qdrant.tech/articles/hybrid-search/)
58. [Hybrid Queries - Qdrant](https://qdrant.tech/documentation/search/hybrid-queries/)
59. [Gemini Embedding: Generalizable Embeddings from Gemini](https://arxiv.org/pdf/2503.07891)
60. [Best Embedding Models 2026: Top APIs & Open-Source](https://checkthat.ai/answers/what-are-the-best-embedding-models)
61. [8 Embedding Models Compared for Production RAG \[2026 Benchmark\]](https://tensoria.fr/en/blog/embedding-models-2026-guide)
62. [Add Reranking to RAG: Cohere vs Voyage vs Zerank-2](https://www.bestaiweb.ai/how-to-add-reranking-to-your-rag-pipeline-with-cohere-rerank-4-pro-voyage-rerank-2-5-and-zerank-2-in-2026/)
63. [Best Rerankers for RAG in 2026 - Tested & Ranked | Mixpeek](https://mixpeek.com/curated-lists/best-rerankers)
64. [Why Did Claude Code Abandon RAG for Agentic Search?](https://zenn.dev/karamage/articles/2514cf04e0d1ac?locale=en)
65. [Settling the RAG Debate: Why Claude Code Dropped Vector DB-Based RAG and the Reality of Code Search - SmartScope](https://smartscope.blog/en/ai-development/practices/rag-debate-agentic-search-code-exploration/)
66. [is rag dead what ai coding agents use instead](https://www.mindstudio.ai/blog/is-rag-dead-what-ai-coding-agents-use-instead)
67. [LightRAG: Simple and Fast Retrieval-Augmented Generation](https://arxiv.org/html/2410.05779v3)
68. [pgvector 0.8.2 Released](https://www.postgresql.org/about/news/pgvector-082-released-3245)
69. [pg\_search: Full text search for PostgreSQL using BM25 / PostgreSQL Extension Network](https://pgxn.org/dist/pg_search/)
70. [Switch to ParadeDB + pg\_search BM25 lexical ranker by robmillersoftware · Pull Request #141 · cahoots-org/contex](https://github.com/cahoots-org/contex/pull/141)
71. [How to Build Hybrid Search with pgvector and BM25 in Postgres | Ben Moataz](https://www.benmoataz.com/posts/hybrid-search-pgvector-bm25)
72. [Hybrid Search - Qdrant](https://qdrant.tech/documentation/tutorials-and-examples/cloud-inference-hybrid-search/)
73. [Project: Building a Hybrid Search Engine - Qdrant](https://qdrant.tech/course/essentials/day-3/pitstop-project/)
74. [Qdrant 1.18 - TurboQuant - Qdrant](https://qdrant.tech/blog/qdrant-1.18.x/)
75. [Releases · weaviate/weaviate-go-client](https://github.com/semi-technologies/weaviate-go-client/releases)
76. [Go | Weaviate Documentation](https://docs.weaviate.io/weaviate/client-libraries/go)
77. [Hybrid search | Weaviate Documentation](https://docs.weaviate.io/weaviate/search/hybrid)
78. [Hybrid Search Explained | Weaviate](https://weaviate.io/blog/hybrid-search-explained)
79. [BM25 results are merged incorrectly into hybrid search results · Issue #10350 · weaviate/weaviate](https://github.com/weaviate/weaviate/issues/10350)
80. [Full Text Search with Milvus Milvus v2.5.x documentation](https://milvus.io/docs/v2.5.x/full_text_search_with_milvus.md)
81. [HybridSearch() - Milvus go sdk v2.6.x/Vector](https://milvus.io/api-reference/go/v2.6.x/Vector/HybridSearch.md)
82. [Releases · asg017/sqlite-vec](https://github.com/asg017/sqlite-vec/releases)
83. [track: monitor sqlite-vec ANN release readiness · Issue #74 · jannekbuengener/sample-brain](https://github.com/jannekbuengener/sample-brain/issues/74)
84. [Hybrid full-text search and vector search with SQLite](https://simonwillison.net/2024/Oct/4/hybrid-full-text-search-and-vector-search-with-sqlite/)
85. [Using sqlite-vec in Go - Alex Garcia](https://alexgarcia.xyz/sqlite-vec/go.html)
86. [Introducing sqlite-vec v0.1.0: a vector search SQLite extension that runs everywhere | Alex Garcia's Blog](https://alexgarcia.xyz/blog/2024/sqlite-vec-stable-release/index.html)
87. [GitHub - philippgille/chromem-go: Embeddable vector database for Go with Chroma-like interface and zero third-party dependencies. In-memory with optional persistence.](https://github.com/philippgille/chromem-go)
88. [lancedb package - github.com/lancedb/lancedb-go/pkg/lancedb - Go Packages](https://pkg.go.dev/github.com/lancedb/lancedb-go/pkg/lancedb)
89. <https://docs.lancedb.com/api-reference>
90. [Embedded Vector Databases for Go in 2026: chromem-go vs sqlite-vec vs Bleve vs LanceDB](https://shaharia.com/blog/choosing-embeddable-vector-database-go-application/)
91. [Retrieval-Enhanced Named Entity Recognition](https://arxiv.org/pdf/2410.13118)
