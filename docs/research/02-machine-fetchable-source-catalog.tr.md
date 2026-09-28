# FRC Yazılım Ekosistemi: MCP Sunucusu İçin Makine Tarafından Çekilebilir Kaynak Kataloğu (2026 sezonu + 2027 Systemcore hazırlığı)

Bir FRC yazılımcısının ihtiyaç duyduğu kaynakların neredeyse tamamı birkaç yapılandırılmış kanaldan otomatik izlenebilir: GitHub Releases (allwpilib, Phoenix-Releases, PhotonVision, Choreo, PathPlanner, AdvantageKit, YAGSL, LimelightLib), vendordep JSON'ları (özellikle merkezi `wpilibsuite/vendor-json-repo`), Maven metadata (`frcmaven.wpi.edu`, `maven.ctr-electronics.com`), Read the Docs / GitBook tabanlı dokümanlar (çoğunun RST/Markdown kaynağı GitHub'da) ve üç veri API'si (FRC Events API, The Blue Alliance v3, Statbotics v3). RSS'e en çok güvenebileceğiniz yerler Chief Delphi (`/latest.rss`), GitHub `releases.atom` feed'leri ve YouTube kanal feed'leridir; FIRST'ün resmi blogu ve birçok vendor sayfası ise RSS'siz ya da feed'i belirsizdir ve hash/ETag tabanlı değişiklik tespiti gerektirir.

## TL;DR

- **Omurga olarak `wpilibsuite/vendor-json-repo` + GitHub Releases API kullanın.** Vendor-json-repo, WPILib'in resmi vendordep kataloğudur (`2026_metadata.json`, `2026/`, `2027_alpha7/` klasörleri); allwpilib, Phoenix, PhotonVision, Choreo, PathPlanner, AdvantageKit, YAGSL ve Limelight'ın release'leri `api.github.com/repos/{owner}/{repo}/releases` ile ETag'li olarak saatlik çekilebilir.
- **2027 geçişi aktif ve kırıcı (breaking):** WPILib 2027.0.0-alpha-7 yayınlandı; Java paketleri `edu.wpi.first` → `org.wpilib`, C++ `frc::` → `wpi::` oldu, SmartDashboard/SendableChooser yerine Telemetry/Tunables API'leri geldi ve allwpilib v2027.0.0-alpha-7 GitHub release notları "It is also necessary to import vendor libraries again, since older vendor libraries must be updated to be compatible with 2027 Alpha 7 projects" diyor. MCP'niz sürüm uyumluluğunu (WPILib yılı ↔ vendordep `frcYear`) birinci sınıf veri olarak tutmalı.
- **Veri API'lerinin hepsi auth ister ve kısıtlıdır:** FRC Events API (`frc-api.firstinspires.org`, HTTP Basic `username:token` Base64, ticari kullanım yasak, `If-Modified-Since` destekli), TBA v3 (`X-TBA-Auth-Key` başlığı, `Last-Modified`, HMAC imzalı webhook'lar), Statbotics v3 (`api.statbotics.io/v3`, açık). Chief Delphi JSON endpoint'leri Cloudflare tarafından otomatik isteklere karşı engellenmiş durumda; forum yöneticisi RSS'i önerip "not blocked via the firewall rules" olduğunu söylüyor.\[1\]

## Key Findings

1. **Merkezi vendordep kataloğu var ve makine okunabilir.** `wpilibsuite/vendor-json-repo` reposu README'sinde, her vendordep JSON'unun "a standalone complete description of all Maven dependencies and related configuration required to include the vendor library in either a C++ or Java GradleRIO robot project" olduğunu belirtiyor; kataloğa ekleme `YEAR_metadata.json` dosyasına bir metadata girdisi eklenerek yapılıyor.\[2\]\[3\] Repo kökünde `2024`, `2025`, `2025beta`, `2026`, `2026beta`, `2027_alpha1`, `2027_alpha5`, `2027_alpha7` klasörleri ve bunlara karşılık gelen `*_metadata.json` dosyaları var.\[2\] WPILib VS Code Dependency Manager'ın gösterdiği liste budur; MCP'nin "hangi vendordep hangi WPILib yılıyla uyumlu" sorusunun birincil cevabı da budur.
2. **2026 sezonu stabil, 2027 alpha hızlı değişiyor.** WPILib 2026.1.1 kickoff sürümü 8 Ocak 2026'da duyuruldu (blog: "171 commits were made since 2025.3.2 by 51 contributors"); ardından 2026.2.1 güncellemesi 2026 saha görselleri ve AprilTag verisini ekledi ve API dokümanları 2026.2.2'yi gösteriyor.\[4\]\[5\]\[6\]\[7\] 2027 tarafında alpha-7 "hundreds of changes since alpha 6" içeriyor, "Systemcore image 14" ve "FIRST Driver Station Alpha 7" gerektiriyor, AprilTag ve CameraServer kütüphaneleri vendordep'lere taşındı ve 2027 dokümanları `docs.wpilib.org/en/latest/` (daha önce `/en/2027/`) altında.\[8\]
3. **Sürüm uyumluluk matrisinin tek resmi kaynağı `wpilibsuite/SystemcoreTesting` reposu.** Bu repo Systemcore/Motioncore alpha-beta testleri içindir ve vendor uyumluluk tablosunu tutar.\[9\] İkincil bir kaynağa (LearnFRC, Ağustos 2026) göre alpha-5/6 için yalnızca CTRE Phoenix 6 v26.50.0-alpha-1 ve REVLib v2027.0.0-alpha-2 uyumlu sürüm listeliyordu; ReduxLib, PathPlannerLib, ChoreoLib ve AdvantageKit için uyumlu sürüm yoktu — ancak bu bilgi ReduxLib için eskidi: Redux 9 Ağustos 2026'da Chief Delphi'de ReduxLib v2027.0.0-alpha-6'yı duyurdu ("This is more or less v2026.1.2 ported to SystemCore 2027_alpha5"). Bu tablo çok hızlı değiştiği için MCP bunu README/markdown dosyalarını commit bazlı izleyerek güncel tutmalı.
4. **Vendordep URL'leri yıl kodlu ve tahmin edilebilir kalıplara sahip** (ör. `REVLib-2026.json`, `Phoenix6-frc2026-latest.json`, `ReduxLib_2026.json`, `ChoreoLib2026.json`, `playingwithfusion2026.json`, `ThriftyLib-2026.json`). Bu, 2027 URL'lerini önceden deneme (probe) imkânı verir — ama tahmin edilen URL'ler doğrulanana kadar "doğrulanmamış" olarak işaretlenmeli (ör. Grapple LaserCAN 2026 URL'i yalnızca kalıptan çıkarılabildi).
5. **Motor verisinde iki "gerçek" var ve birbirini tutmuyor.** WPILib `DCMotor` sabitleri (NEO: 2.6 Nm / 105 A) ile ReCalc'in CTRE dyno verileri (NEO: 4.20 Nm / 216 A) NEO ve NEO Vortex için büyük farklılık gösteriyor; Kraken X44, Minion ve Falcon 500'de ise iki kaynak neredeyse aynı.\[10\]\[11\] MCP her iki kaynağı da ayrı etiketle sunmalı, tek bir değer seçmemeli.
6. **Redux Robotics için risk notu:** Redux, 8 Ekim 2025'te Chief Delphi'deki "The End of Redux Robotics" gönderisinde "over the coming weeks we will be shutting down Redux Robotics" dedi; ancak 6 Ocak 2026 tarihli "Redux Robotics 2026" gönderisinde "we have identified a stable path forward. While Cu60 and Nitrate remain gone, we are excited to continue to be able to serve the FRC market" diyerek kararı tersine çevirdi. ReduxLib'in güncel sürümü dokümanlara göre 2026.1.2\[12\] ve kaynak kodu `Redux-Robotics/canandrepo-public` altında tamamen açık.\[12\]

## Details

### 1. Resmi WPILib kaynakları

| Kaynak | Ana URL | Makine-çekilebilir endpoint(ler) | Format | Güncelleme döngüsü | Auth / kısıt | Önerilen fetch stratejisi |
|---|---|---|---|---|---|---|
| WPILib docs (frc-docs) | https://docs.wpilib.org/en/stable/ (2026), https://docs.wpilib.org/en/latest/ (2027) | Kaynak repo: `github.com/wpilibsuite/frc-docs` (alt ajana göre `wpilibsuite/wpilib-docs` olarak yeniden adlandırılmış olabilir; GitHub eski adı yönlendirir — ⚠️ doğrulayın). Read the Docs projesi slug'ı `frc-docs` (sayfa meta verisinde teyitli). Sitemap: `https://docs.wpilib.org/sitemap.xml` (⚠️ RTD standardı, doğrulanmadı). RTD, sayfa kaynaklarını `/_sources/...rst.txt` olarak sunar (CTRE docs'ta teyitli kalıp). | HTML, RST | Kickoff (Ocak) büyük güncelleme, sezon boyunca sürekli; 2027 için `latest` sürekli değişir | Açık, CC lisanslı içerik (⚠️ lisansı repo'dan teyit edin) | RST'yi GitHub'dan çekin (git clone/`git fetch` veya commits API); HTML scraping yerine. Günlük. Sayfa `article:modified_time` meta'sı değişiklik tespiti için kullanılabilir. |
| Known Issues / New for 2026 / 2027 changelog | `.../docs/yearly-overview/known-issues.html`, `.../yearly-changelog.html` | Aynı RST repo'su | HTML/RST | Release'lerle birlikte | Açık | 6 saatte bir hash kontrolü (sezon içi) |
| allwpilib | https://github.com/wpilibsuite/allwpilib | `https://api.github.com/repos/wpilibsuite/allwpilib/releases`, Atom: `https://github.com/wpilibsuite/allwpilib/releases.atom` | JSON / Atom | Beta sonbahar (2026 beta testi 2 Aralık 2025'te duyuruldu),\[13\] kickoff release Ocak, sezon içi 2026.x.y güncellemeleri, 2027 alpha'lar ~aylık | GitHub API rate limit | Saatlik, `If-None-Match` (ETag) ile; `prerelease` alanıyla alpha/beta ayırın |
| Java API (Javadoc) | https://github.wpilib.org/allwpilib/docs/release/java/ (beta: `/docs/beta/java/`) | Statik HTML; Javadoc `element-list`/`package-list` dosyası | HTML | Her release | Açık | Release olayı tetiklediğinde yeniden indeksleyin, polling gerekmez |
| C++ API (Doxygen) | https://github.wpilib.org/allwpilib/docs/release/cpp/ | Doxygen HTML (ör. `_d_c_motor_8h_source.html`) | HTML | Her release | Açık | Release tetikli |
| WPILib Maven | https://frcmaven.wpi.edu/artifactory/release/ ve `/artifactory/development/` | `.../release/edu/wpi/first/wpilibj/wpilibj-java/maven-metadata.xml` kalıbı (Artifactory dizin listelerinde `maven-metadata.xml` teyitli). Artefakt koordinatları `allwpilib/MavenArtifacts.md` içinde. | XML | Her release; development her `main` commit'inde |\[14\]\[15\] Açık | Günlük `maven-metadata.xml` + `<lastUpdated>`; development repo'yu izlemeyin (çok gürültülü) |
| Vendor JSON Repo | https://github.com/wpilibsuite/vendor-json-repo | `raw.githubusercontent.com/wpilibsuite/vendor-json-repo/main/2026_metadata.json`, `.../2026beta_metadata.json`, `.../2027_alpha7_metadata.json`; klasör listesi için `api.github.com/repos/wpilibsuite/vendor-json-repo/contents/2026` (⚠️ github.com ağaç sayfası robots.txt ile otomatik erişime kapalı; raw ve API kullanın) | JSON | Vendor PR'larıyla (sezon içi haftalık) | GitHub rate limit | 6 saatte bir `commits?path=` veya raw dosyada ETag |
| WPILib blog | https://wpilib.org/blog | Squarespace'in yerleşik feed'i: `https://wpilib.org/blog?format=rss` (⚠️ Squarespace kalıbı; site Squarespace üzerinde, ama feed doğrulanmadı). Chief Delphi'de "[WPILib Blog]" başlıklı çapraz gönderiler de var. | RSS | Birkaç ayda bir (beta, kickoff, Championship wrap-up) | Açık | Günlük |
| RobotPy | https://robotpy.readthedocs.io/projects/robotpy/en/stable/ | PyPI JSON API: `https://pypi.org/pypi/{paket}/json` (`robotpy`, `wpilib`, `phoenix6`, `photonlibpy`, `sleipnirgroup-choreolib`, `robotpy-rev` vb.); PyPI RSS: `https://pypi.org/rss/project/{paket}/releases.xml` | JSON / RSS | WPILib ile senkron | Açık | 6 saatte bir PyPI JSON |
| SystemcoreTesting | https://github.com/wpilibsuite/SystemcoreTesting | README.md, AdvantageKit.md vb. markdown; Issues (known issues) | Markdown | 2027 alpha boyunca sık | GitHub rate limit | Commits API ile 6 saatte bir; issues için `?since=` |

**Vendordep formatı notu:** WPILib dokümanına göre vendordep JSON'u isteğe bağlı olarak bir "online update location" içerir (`jsonUrl`), bu da VS Code'daki "To Latest" butonunu besler. CLI eşdeğeri `gradlew vendordep --url=<url>`; `FRCLOCAL/Filename.json` ile yerel WPILib klasöründen çekilebilir. Yerel kurulum yolu `~/wpilib/YYYY/vendordeps` (Windows'ta `C:\Users\Public`).\[12\]\[16\]\[17\]

### 2. FIRST resmi kaynakları

| Kaynak | Ana URL | Endpoint | Format | Döngü | Auth / kısıt | Strateji |
|---|---|---|---|---|---|---|
| FRC blog | https://community.firstinspires.org/topic/frc | FRC blogu Haziran 2024'te FIRST Community Blog'a taşındı; eski `https://www.firstinspires.org/robotics/frc/blog-rss` feed'i artık yeni gönderi almıyor. Yeni blog için resmi yol e-posta aboneliği;\[18\]\[19\] RSS feed'i doğrulanamadı (⚠️ `/topic/frc/rss` gibi bir kalıbı deneyin). | HTML | Haftalık-aylık; kickoff öncesi yoğun | Açık | Liste sayfasını günlük hash'leyin; Chief Delphi'deki "[FRC Blog]" çapraz gönderileri RSS alternatifi |
| Game Manual & Team Updates | https://www.firstinspires.org/resources/library/frc/season-materials | PDF/HTML; stabil bir API yok | PDF, HTML | Kickoff (Ocak başı) + sezon içi Team Update'ler (genelde Salı/Cuma) | Açık okuma; FIRST telif | Sezon içi günde 2 kez sayfa hash + PDF `ETag/Last-Modified`; PDF'ten metin çıkarıp bölüm bazlı diff |
| Q&A | https://frc-qa.firstinspires.org/ | HTML | HTML | Kickoff sonrası sürekli | Okuma açık (⚠️ ToS kontrol edin) | Sezon içi günlük liste scraping |
| FRC Events API | Kayıt: https://frc-events.firstinspires.org/services/API; istekler: `https://frc-api.firstinspires.org/`; dokümantasyon: https://frc-api-docs.firstinspires.org/ | REST (v3 güncel sürüm) | JSON (XML de destekli) | Etkinlik sırasında dakikalık | `Authorization: Basic base64(username:AuthorizationKey)`; token FIRST tarafından kapatılabilir ("Account Disabled... for excessive traffic or abuse"); token'ı paylaşmak engellenmeye yol açar; API'yi kullanan uygulamalar kayıt sayfasına geri link vermeli (Terms of Use)\[20\]\[21\]\[22\] | `If-Modified-Since` / `Last-Modified` zorunlu kullanın; etkinlik yokken günlük |
| Control System docs | docs.wpilib.org → Hardware Component Overview, roboRIO imaging (roboRIO 1 & 2), radio programming; 2027 için Systemcore Introduction | frc-docs RST | RST/HTML | Kickoff (FRC Game Tools/roboRIO image) | Açık | frc-docs repo ile birlikte |
| FRC Game Tools / Driver Station | NI Game Tools (2026 roboRIO image için gerekli); 2027 FIRST Driver Station (alpha) | Release notları; 2027 DS için WPILib blog/GitHub | HTML | Yıllık + yamalar | Açık | Aylık kontrol |

### 3. Vendor kütüphaneleri

| Vendor / Kütüphane | Doküman | Vendordep JSON (2026) | Diğer makine endpoint'leri | Not |
|---|---|---|---|---|
| **CTRE Phoenix 6** | https://v6.docs.ctr-electronics.com/en/latest/ (eski alias `pro.docs...`); yıllık changelog: `.../docs/yearly-changes/yearly-changelog.html` | `https://maven.ctr-electronics.com/release/com/ctre/phoenix6/latest/Phoenix6-frc2026-latest.json`; replay: `.../Phoenix6-replay-frc2026-latest.json`;\[23\]\[24\] beta: `...-frc2026-beta-latest.json`\[25\]\[26\] | Releases: `api.github.com/repos/CrossTheRoadElec/Phoenix-Releases/releases`; doküman kaynağı RST: `CrossTheRoadElec/Phoenix6-Documentation` (`source/docs/...rst`); API: `https://api.ctr-electronics.com/phoenix6/latest/java/`, `/cpp/`, `/python/`; PyPI `phoenix6`;\[25\]\[27\] PDF: `https://v6.docs.ctr-electronics.com/_/downloads/en/latest/pdf/` | 2026'da `<Device>(int id, String canbus)` kurucuları kullanımdan kaldırıldı, 2027'de silinecek; minimum C++20.\[28\] 2027 alpha: v26.50.0-alpha-1 (ikincil kaynak). Tuner X Swerve Generator dokümanı `.../docs/tuner/tuner-swerve/index.html`.\[29\] |
| **CTRE Phoenix 5** | aynı portal | `https://maven.ctr-electronics.com/release/com/ctre/phoenix/Phoenix5-frc2026-latest.json` (+ replay)\[23\]\[24\] | — | Yalnızca v5 cihaz kullananlar da v6 vendordep'i eklemeli.\[26\] |
| **REV REVLib** | https://docs.revrobotics.com/revlib | `https://software-metadata.revrobotics.com/REVLib-2026.json` \[23\]\[30\] | GitHub org `github.com/REVrobotics` (ör. `REVrobotics/SystemCoreTesting`, `node-revlog-converter`, `robotpy-rev` fork'u);\[31\] REV Hardware Client offline kurulum | 2027 alpha: REVLib v2027.0.0-alpha-2 (ikincil kaynak). REVLib 2026'nın kendi resmi loglaması var; `.revlog` → `.wpilog` dönüştürücüsü mevcut.\[31\]\[32\]\[33\] |
| **Redux (ReduxLib)** | https://docs.reduxrobotics.com/reduxlib ; Canandmag `/canandmag/`; Java API `https://apidocs.reduxrobotics.com/current/java/`, C++ `/current/cpp/` | `https://frcsdk.reduxrobotics.com/ReduxLib_2026.json` \[12\] | Kaynak: `github.com/Redux-Robotics/canandrepo-public` | Güncel 2026.1.2;\[12\] Canandmag (14-bit, CAN + PWM), Canandgyro (500 Hz odometri), Canandcolor, Zinc-V.\[34\]\[35\] Şirket Ekim 2025'te kapanış duyurdu, Ocak 2026'da FRC'ye devam kararı aldı (Cu60 ve Nitrate ürünleri iptal); 2027 için ReduxLib v2027.0.0-alpha-6 yayımlandı. |
| **Studica / navX** | Studica NavX GitHub: `github.com/Studica-Robotics/NavX` | `https://dev.studica.com/maven/release/2026/json/Studica-2026.0.0.json` \[36\]\[37\] | — | Vendordep artık "Studica" adında; eski "StudicaLib" ile birlikte kullanılamaz.\[37\] Kauai Labs'ın eski `kauailabs.com/dist/frc/...` URL'leri eskidi. |
| **Playing With Fusion** | playingwithfusion.com (docid=1205) | `https://www.playingwithfusion.com/frc/playingwithfusion2026.json` \[38\] | — | Venom, ToF sensörleri. |
| **Grapple (LaserCAN)** | grapplerobotics.au; LaserCAN docs | ⚠️ 2026 doğrulanmadı; kalıp: `https://storage.googleapis.com/grapple-frc-maven/libgrapplefrc2026.json` (2025 için `libgrapplefrc2025.json` teyitli)\[39\]\[40\] | `GrappleRobotics/libgrapplefrc` repo'sunda `depjson/`\[41\] | Probe edin; 404 ise vendor-json-repo'daki girdiye bakın. |
| **ThriftyBot (ThriftyLib)** | docs.home.thethriftybot.com | `https://docs.home.thethriftybot.com/ThriftyLib-2026.json` \[42\] | vendor-json-repo'ya eklendi (PR #222)\[43\] | Thrifty Nova motor kontrolcüsü; firmware notları ThriftyLib ≥ v2026.1.0 istiyor.\[44\] |
| **Limelight** | https://docs.limelightvision.io/docs/docs-limelight/apis/limelight-lib | LimelightHelpers klasik olarak tek dosya (vendordep değil): `LimelightVision/limelightlib-wpijava`, `limelightlib-wpicpp` releases (v1.14, "REQUIRES LLOS 2026.0 OR LATER").\[45\]\[46\] 2026 yazında "LimelightLib 2" vendordep'e (`com.limelightvision.Limelight`) geçiş başladı (⚠️ resmi vendordep URL'ini doğrulayamadım; bir topluluk PR'ı yayımlanmış URL'in erişilemediğini not ediyor).\[47\] | Javadoc: `https://limelightlib-wpijava-reference.limelightvision.io`, C++: `https://limelightlib-wpicpp-reference.limelightvision.io`; releases API\[48\]\[49\] | Limelight aynı zamanda 2027 Systemcore'un üreticisi; `.llupdate` imajları `systemcore-os-public` repo'sunda (SystemcoreTesting README).\[9\]\[50\] Topluluk alternatifi: YASS `YALL`.\[51\] |
| **PhotonVision** | https://docs.photonvision.org/en/latest/ | Repo içindeki release asset'i (ör. `photonlib-v2026.3.4.json`); eski sabit URL `https://maven.photonvision.org/repository/internal/org/photonvision/photonlib-json/1.0/photonlib-json-1.0.json` \[23\]\[52\]\[53\] | `api.github.com/repos/PhotonVision/photonvision/releases` (2026 ilk tam sürüm 2026.1.1; v2026.3.4 10 Nisan 2026);\[52\] PyPI `photonlibpy`; docs kaynağı RST (`photonvision-docs` repo, sonra ana repoya taşınmış görünüyor) | Release asset'leri Limelight 2/3/3G imajlarını da içeriyor.\[52\] |
| **AndyMark, WCP, SDS** | andymark.com, wcproducts.com, swervedrivespecialties.com | Vendordep yok (donanım + CAD/spec sayfaları) | ⚠️ Shopify tabanlı mağazalarda `/products/{handle}.json` genellikle çalışır ama doğrulanmadı | Spec'ler için haftalık sayfa hash'i yeterli. |

### 4. Topluluk kütüphaneleri ve araçları

| Kaynak | Doküman | Vendordep / paket | Makine endpoint | Not |
|---|---|---|---|---|
| **YAGSL** | Yeni: https://yagsl.yassrobotics.com/ (eski: `docs.yagsl.com`, `yagsl.gitbook.io`) | Güncel doküman: `https://yet-another-software-suite.github.io/YAGSL/yagsl/yagsl.json`; bağımlılık sayfası hâlâ `.../YAGSL/yagsl.json` gösteriyor\[24\]\[29\] (⚠️ iki URL çelişiyor — ikisini de probe edin) | GitBook: `https://yagsl.yassrobotics.com/llms.txt` ve her sayfaya `.md` ekleyerek Markdown;\[29\] kaynak `thenetworkgrinch/YAGSL-gitbook`; kod `Yet-Another-Software-Suite/YAGSL` | 2026.8.05 ile JSON şeması değişti; YAGSL artık bir YAMS `SwerveDrive` üreten JSON ayrıştırıcısı. Config generator: `config.yagsl.com`.\[29\] YAGSL; maple-sim, Phoenix 6/5 replay, REVLib, Studica, ReduxLib vendordep'lerinin hepsinin kurulmasını istiyor.\[24\] Bir takım 2026 başında vendordep'teki `frcYear` 2025 kaldığı için "invalid year" hatası yaşadı\[54\] — MCP'niz `frcYear` doğrulaması yapmalı. |
| **PathPlanner / PathPlannerLib** | https://pathplanner.dev | `https://3015rangerrobotics.github.io/pathplannerlib/PathplannerLib.json` (ayrıca eski yıllar için legacy JSON'lar)\[55\] | Releases: `mjansen4857/pathplanner` (⚠️ owner'ı API'den teyit edin); PyPI `robotpy-pathplannerlib` (⚠️) | 2026 sürümü 2026.1.2; LabVIEW sürümü resmi değil.\[55\]\[56\] |
| **Choreo / ChoreoLib** | https://choreo.autos | `https://choreo.autos/lib/ChoreoLib2026.json` \[57\] | `api.github.com/repos/SleipnirGroup/Choreo/releases`; PyPI `sleipnirgroup-choreolib` (2026.0.1);\[58\] docs Markdown repo'da (`docs/`) | 2027 alpha-3 yalnızca Commands v2'ye bağlı; Commands v3 ile çakışıyor (`conflictsWith`).\[59\] |
| **AdvantageKit** | docs.advantagekit.org | `https://github.com/Mechanical-Advantage/AdvantageKit/releases/latest/download/AdvantageKit.json` (2027 alpha için sabitlenmiş sürüm URL'i, ör. `.../download/v27.0.0-alpha-5/AdvantageKit.json`)\[60\]\[61\] | Releases API | 2027 notları SystemcoreTesting `AdvantageKit.md`'de (ör. `LoggedDashboardChooser` → `LoggedNetworkChooser`).\[61\] |
| **AdvantageScope** | docs.advantagescope.org; WPILib docs'ta da sayfası var | Masaüstü uygulaması | `Mechanical-Advantage/AdvantageScope` releases | WPILib ile birlikte dağıtılıyor. |
| **URCL** | docs.advantagescope.org/more-features/urcl/ | `https://raw.githubusercontent.com/Mechanical-Advantage/URCL/main/URCL.json` \[32\] | Repo commits | REVLib 2026'nın yerleşik loglaması nedeniyle önemi azaldı. |
| **maple-sim** | shenzhen-robotics-alliance.github.io/maple-sim | `https://shenzhen-robotics-alliance.github.io/maple-sim/vendordep/maple-sim.json` \[36\]\[62\] | Releases API | WPILib 2026.2.1'e karşı derlendi; fizik simülasyonu.\[63\]\[64\] |
| **Elastic, Shuffleboard, Glass, SmartDashboard** | docs.wpilib.org → Dashboards | WPILib ile gelir (Elastic topluluk projesi) | Elastic GitHub releases (⚠️ owner: `Gold872/elastic-dashboard`, doğrulanmadı) | 2027'de SmartDashboard/Sendable API'leri Telemetry/Tunables ile değiştirildi.\[65\] |
| **Tuner X Swerve Generator** | CTRE docs `tuner-swerve` | Tuner X uygulamasında | CTRE docs RST | Yalnızca CTRE donanımı (Falcon/Kraken/TalonFXS + Pigeon 2 + CANcoder) için; YAGSL bile bunu önce öneriyor.\[29\] |
| **Takım referans kodları** | 254, 6328 (Mechanical Advantage), 1678, 971 vb. | — | `api.github.com/users/{org}/repos?sort=pushed`; ör. `Team254`, `Mechanical-Advantage` (⚠️ org adlarını teyit edin) | Sezon sonrası (Mayıs-Haziran) kod yayınları; haftalık. |
| **Topluluk vendordep toplayıcıları** | ör. `TexasTorque/TorqueVendordeps` | PathplannerLib 2026.1.2, Phoenix6 26.1.0, REVLib 2026.0.0 vb. listeler\[56\] | Raw JSON | Çapraz doğrulama için faydalı, birincil kaynak değil. |
| **BearSwerve** | — | — | ⚠️ 2026 için aktif bir kaynak bulamadım; muhtemelen bakımsız | Katalogda "legacy" olarak işaretleyin. |

### 5. Donanım spec verileri

**Motor sabitleri — iki kaynak karşılaştırması (12 V):**

| Motor | WPILib `DCMotor` (stall torque / stall A / free A / free rpm) | ReCalc (reca.lc/motors) | Yorum |
|---|---|---|---|
| Kraken X60 | 7.09 Nm / 366 A / 2 A / 6000 | 7.16 Nm / 374 A / 2.8 A / 6065 (CTRE) | Uyumlu |
| Kraken X60 FOC | 9.37 / 483 / 2 / 5800 | 9.36 / 476 / 3.5 / 5784 | Uyumlu |
| Kraken X44 | 4.11 / 279 / 2 / 7758 | 4.11 / 279 / 3.2 / 7757 | Aynı (CTRE dyno) |
| Kraken X44 FOC | 5.01 / 329 / 2 / 7368 | 5.01 / 329 / 3.2 / 7367 | Aynı |
| Minion | 3.17 / 211 / 2 / 7704 | 3.17 / 212 / 2.2 / 7703 | Aynı |
| Falcon 500 | 4.69 / 257 / 1.5 / 6380 | 4.69 / 257 / 1.5 / 6380 | Aynı |
| NEO | 2.6 / 105 / 1.8 / 5676 | 4.20 / 216 / 1.8 / 5906 (CTRE) | **Büyük fark** |
| NEO Vortex | 3.60 / 211 / 3.615 / 6784 | 5.96 / 391 / 5.4 / 6825 (CTRE) | **Büyük fark** |
| NEO 550 | 0.97 / 100 / 1.4 / 11000 | 1.08 / 111 / 1.1 / 11710 (REV) | Orta fark |

Yorum: WPILib'in NEO/Vortex değerleri eski REV spec sayfalarından, ReCalc'inkiler ise CTRE'nin dinamometre ölçümlerinden geliyor.\[10\]\[11\] Simülasyon için WPILib değerleri (kodla tutarlılık), mekanizma boyutlandırma için ReCalc/CTRE dyno değerleri daha gerçekçi olabilir; MCP her iki değeri kaynak etiketiyle döndürmeli.

**Makine-çekilebilir kaynaklar:**
- WPILib Java kaynağı: `wpimath/src/main/java/edu/wpi/first/math/system/plant/DCMotor.java` (allwpilib; `main` 2027'ye geçtiği için `v2026.2.1` gibi bir tag'e sabitleyin — 2027'de paket `org.wpilib` altına taşındı). C++: `frc/system/plant/DCMotor.h` (Doxygen'de teyitli). Kurucu sırası: nominal voltaj, stall torque, stall current, free current, free speed.\[10\] RobotPy: `wpimath.system.plant.DCMotor.NEO(numMotors)`.\[66\]
- ReCalc: `github.com/tervay/recalc` (TypeScript; motor modelleri muhtemelen `src/common/models/Motor*` altında — ⚠️ dosya yolu doğrulanmadı); tablo: https://www.reca.lc/motors. \[11\]\[67\]
- CTRE dyno verisi: `motors.ctr-electronics.com/dyno/dynometer-testing` (WPILib kodunda kaynak olarak geçiyor).\[10\]
- Sensörler (CANcoder, Pigeon 2, Canandmag/Canandgyro, REV Through Bore, navX, LaserCAN, PWF ToF), motor kontrolcüleri (TalonFX, Talon FXS, SPARK MAX, SPARK Flex, Thrifty Nova) ve swerve modülleri (SDS MK4i/MK4n/MK5, WCP Swerve X, Thrifty Swerve, REV MAXSwerve) için yapılandırılmış API yok; spec'ler vendor dokümanları (çoğu GitBook/RTD) ve ürün sayfalarından çekilmeli. YAGSL'in referans dokümanındaki "supported hardware type strings" tablosu, hangi cihazların yazılım tarafından desteklendiğine dair iyi bir makine-dostu indekstir.

### 6. Topluluk ve haber

| Kaynak | Endpoint | Format | Auth / kısıt | Strateji |
|---|---|---|---|---|
| Chief Delphi | `https://www.chiefdelphi.com/latest.rss` (teyitli, FRC Discord'daki Dozer botu bunu kullanıyor);\[1\] kategori RSS'leri Discourse kalıbıyla `/c/{slug}/{id}.rss` (ör. Programming) ve tag RSS'leri `/tag/{tag}.rss` (⚠️ kategori ID'lerini tarayıcıdan teyit edin); topluluk MCP'si `rylero/chiefdelphi-mcp` tag RSS + Open Alliance kategori RSS + sitemap kullanıyor\[68\] | RSS | JSON endpoint'leri (`/posts.json`, `/latest.json`) Cloudflare bot korumasıyla engellenmiş; otomatik gönderi yasak; RSS engelli değil\[1\] | 15-30 dk'da bir RSS, `If-Modified-Since`; konuyu okumak gerekirse düşük hızda ve önbellekli |
| r/FRC | `https://www.reddit.com/r/FRC/.rss` (⚠️ Reddit standart kalıbı, doğrulanmadı) | Atom | Reddit özel User-Agent ister, agresif rate limit | 1 saatte bir |
| FRC Discord sunucuları | Fetch edilemez (auth + ToS) | — | Bot token + sunucu izni gerekli | Katalogda "manuel" olarak işaretleyin; Chief Delphi RSS büyük kısmını yansıtır |
| The Blue Alliance v3 | `https://www.thebluealliance.com/api/v3` ; docs `/apidocs`; webhooks `/apidocs/webhooks` | JSON | `X-TBA-Auth-Key` (hesap panelinden "Read API Keys");\[69\] `Last-Modified` her yanıtta var; webhook'lar `X-TBA-HMAC` (SHA256) ile imzalı, 10 sn timeout; "Notification Firehose" tüm yılın etkinliklerini gönderir\[70\]\[71\]\[72\] | Polling yerine webhook tercih edin; polling gerekiyorsa `If-Modified-Since` |
| Statbotics | REST v3: `https://api.statbotics.io/v3` ; docs `https://www.statbotics.io/docs/rest` (OAS 3.1) ; Python `statbotics==3.0.0`\[73\]\[74\]\[75\] | JSON | Auth yok; tek kişi finanse ediyor — nazik olun\[74\] | Etkinlik sırasında 5-10 dk, aksi halde günlük |
| TBA / WPILib / FIRST YouTube kanalları | `https://www.youtube.com/feeds/videos.xml?channel_id={ID}` (YouTube standart kalıbı) | Atom | Açık | 6 saatte bir; kanal ID'lerini bir kez çözüp saklayın |
| Blog'lar (TBA blog WordPress, Statbotics blog) | `blog.thebluealliance.com/feed/` (⚠️ WordPress kalıbı) | RSS | Açık | Günlük |

### 7. Simülasyon, CAN ve uyumluluk

- **Simülasyon:** WPILib sim (HALSim, `DCMotor` + `LinearSystemSim`), CTRE Phoenix 6 sim + Hoot Replay (replay vendordep'leri), maple-sim (fizik), AdvantageKit replay, PhotonVision sim (`PhotonCameraSim`). Hepsi yukarıdaki GitHub/RTD kaynaklarından izlenir.
- **CAN/CANivore:** CTRE 2026'da `CANBus` nesnesi zorunluluğuna hazırlık yapıyor (2027'de açık CAN bus bildirimi gerekli); CANivore ve Linux apt kaynakları (`/etc/apt/sources.list.d/ctr${YEAR}.list`) CTRE docs'ta.\[76\]
- **Uyumluluk matrisi:** (a) vendor-json-repo `YEAR_metadata.json`; (b) vendordep JSON'daki `frcYear`; (c) SystemcoreTesting uyumluluk tablosu; (d) her vendor'ın "New for YEAR" sayfası. Beta programları: WPILib beta testi sonbaharda (2026 için 2 Aralık 2025 duyurusu),\[13\] CTRE `-beta-latest.json`, vendor-json-repo `2026beta/`.

## Recommendations

### (a) Önceliklendirilmiş kritik kaynaklar

1. `wpilibsuite/allwpilib` releases (Atom + API) — sürüm kalbi.
2. `wpilibsuite/vendor-json-repo` `2026_metadata.json` + `2027_alpha7_metadata.json` — vendordep kataloğu ve uyumluluk.
3. frc-docs RST kaynağı (stable + latest) — Known Issues ve yearly changelog dahil.
4. CTRE Phoenix 6 (Phoenix-Releases + `Phoenix6-frc2026-latest.json` + Phoenix6-Documentation RST).
5. REVLib (`REVLib-2026.json` + docs.revrobotics.com).
6. YAGSL (`yagsl.json` + `llms.txt` Markdown dokümanları).
7. PathPlanner, Choreo, AdvantageKit, PhotonVision, Limelight releases.
8. `wpilibsuite/SystemcoreTesting` — 2027 hazırlığı için tek yetkili uyumluluk kaynağı.
9. Chief Delphi `latest.rss` + Programming kategori RSS'i.
10. FRC Events API / TBA v3 / Statbotics (etkinlik verisi; robot kodu için ikincil).
11. Game Manual + Team Updates (kickoff sonrası kritik, özellikle robot kuralları ve AprilTag yerleşimi).

### (b) Feed'i olmayan önemli kaynaklar ve alternatif değişiklik tespiti

| Kaynak | Yöntem |
|---|---|
| Game Manual / Team Updates | Season-materials sayfasını hash'le; PDF'lere `HEAD` ile `ETag/Last-Modified`; PDF metnini çıkarıp diff |
| FIRST Community Blog (FRC) | Liste sayfasını hash'le + Chief Delphi "[FRC Blog]" çapraz gönderileri |
| Vendordep JSON'ları | `GET` + `If-None-Match`; gövdede `version` alanını karşılaştır |
| Vendor docs (REV GitBook, Redux, Limelight, YAGSL) | GitBook sitelerinde `llms.txt` / `.md` sürümleri; sitemap `lastmod`; yoksa normalize edilmiş metin hash'i |
| RTD dokümanları (WPILib, CTRE, PhotonVision) | Kaynak repo commits API (`?path=source/`), en verimlisi |
| Maven repo'ları | `maven-metadata.xml` `<versioning><latest>`/`<lastUpdated>` |
| Q&A sistemi | Liste sayfası scraping, soru ID'si bazlı delta |
| Motor/ürün spec sayfaları | Haftalık hash; değişince insan onaylı güncelleme |
| Discord | Fetch edilemez; kapsam dışı |

### (c) Go ile pratik notlar

- **Vendordep JSON şeması (Go struct):** `fileName`, `name`, `version`, `frcYear` (string; ör. `"2026"`, `"2026beta"`), `uuid`, `mavenUrls` ([]string), `jsonUrl`, `conflictsWith` ([]{uuid, errorMessage, offlineFileName}), `javaDependencies` ([]{groupId, artifactId, version}), `jniDependencies` ([]{groupId, artifactId, version, isJar, skipInvalidPlatforms, validPlatforms, simMode}), `cppDependencies` ([]{groupId, artifactId, version, libName, headerClassifier, sharedLibrary, skipInvalidPlatforms, binaryPlatforms, simMode}). Bilinmeyen alanlar için `json.RawMessage` veya `map[string]any` fallback'i tutun; alan listesi vendor-json-repo README'sindeki metadata anahtarlarıyla çapraz kontrol edilmeli (⚠️ `requires`/`simMode` gibi alanlar yıllara göre eklendi). Maven koordinatından URL: `{mavenUrl}/{groupId'de . → /}/{artifactId}/{version}/` ve `.../{artifactId}/maven-metadata.xml`.
- **GitHub API:** GitHub Docs'a ("Rate limits for the REST API") göre kimliksiz istekler için birincil limit saatte 60, kimlikli istekler için kişisel limit saatte 5.000'dir (`X-RateLimit-Remaining` başlığını okuyun). `If-None-Match` ile dönen 304 yanıtları limitten düşmez. `releases?per_page=10` + `prerelease` alanı; tek istekle birden çok repo için GraphQL (`repository { releases(last:5) }`) düşünün. Atom (`/releases.atom`) auth gerektirmez, limit dostudur.
- **HTTP istemcisi:** her kaynak için `ETag`, `Last-Modified`, gövde SHA-256'sını SQLite/bbolt'ta saklayın; `If-None-Match`/`If-Modified-Since` gönderin; `golang.org/x/time/rate` ile host bazlı limit; RSS/Atom için `github.com/mmcdole/gofeed`. Anlamlı bir `User-Agent` (ör. `frc-robot-builder-mcp/1.0 (+iletişim)`) kullanın — TBA ayrıca istemci kimliğini (`X-TBA-App-Id`) talep etmiştir.\[70\]
- **FRC Events API:** `req.SetBasicAuth(username, authKey)` doğrudan doğru başlığı üretir. Token'ı asla MCP çıktısına veya repo'ya koymayın (paylaşım = engellenme). 404 yalnızca "sezon+etkinlik kodu bulunamadı" anlamına gelir; hatalı parametrede 400/501 döner.\[21\]\[77\]
- **TBA v3:** `X-TBA-Auth-Key` başlığı; webhook alıcısında `X-TBA-HMAC`'ı `crypto/hmac` + `sha256` ile doğrulayın ve 10 sn içinde 200 dönün (işi kuyruğa atın).\[72\]
- **Discourse (Chief Delphi):** JSON'a güvenmeyin; RSS kullanın.\[1\] `latest.rss` 15-30 dk; tek konu için `/t/{slug}/{id}.rss` (⚠️ Discourse kalıbı) konuya özgü feed verir.
- **Sezon-duyarlı zamanlayıcı:** Eylül-Aralık (beta/alpha dönemi) → allwpilib/SystemcoreTesting/vendor-json-repo saatlik; Ocak-Nisan (sezon) → Team Updates günde 2, vendordep'ler 6 saatte bir, etkinlik API'leri canlı; Mayıs-Ağustos → günlük/haftalık.
- **Sürüm kanalı modeli:** her kaynağı `{season: 2026|2027, channel: stable|beta|alpha}` ile etiketleyin; LLM'e yanıt verirken kullanıcının WPILib sürümünü sorup yalnızca uyumlu kanalı döndürün.

## Caveats

- **Doğrulanmamış URL'ler (⚠️ ile işaretli):** WPILib blog Squarespace RSS'i, `docs.wpilib.org/sitemap.xml`, FIRST Community Blog RSS'i, Grapple 2026 vendordep'i, LimelightLib 2 vendordep URL'i, r/FRC RSS, Chief Delphi kategori RSS ID'leri, ReCalc motor veri dosyası yolu, Elastic/PathPlanner GitHub owner'ları. Bunları ilk çalıştırmada probe edip sonuca göre kataloğa "verified" bayrağı ekleyin.
- **Çelişkiler:** YAGSL için iki farklı vendordep URL'i (`/YAGSL/yagsl.json` ve `/YAGSL/yagsl/yagsl.json`) resmi sayfalarda birlikte geçiyor. 2027 dokümanlarının yeri çelişkili: `wpilibsuite/SystemcoreTesting` README'si hâlâ "all updated documentation for 2027 WPILib changes... can be found on the '2027' version of the WPILib Docs site: https://docs.wpilib.org/en/2027/" derken alpha-7 release notları `/en/latest/` adresini gösteriyor. frc-docs repo adının `wpilib-docs` olarak değiştiği bilgisi ikincil.
- **İkincil kaynaklar:** 2027 alpha-5/6 uyumluluk tablosu birincil kaynaktan teyit edilmedi ve ReduxLib için eskidi (Redux, Ağustos 2026'da v2027.0.0-alpha-6'yı yayımladı). LearnFRC gibi özet siteler, Ağustos 2026 itibarıyla Systemcore fiyatı, satış tarihi ve 2027 robot kurallarının henüz yayımlanmadığını not ediyor\[33\] — bu konulardaki her iddia spekülasyondur.
- **Hız:** 2027 alpha ekosistemi aylık kırıcı değişiklik alıyor; bu rapordaki sürüm numaraları (alpha-7, Phoenix 26.50.0-alpha-1, REVLib 2027.0.0-alpha-2) birkaç hafta içinde eskiyebilir. MCP, sürüm numaralarını statik veri olarak değil, her zaman canlı kaynaktan sunmalı.
- **ToS:** FRC Events API verisi ticari amaçla kullanılamaz;\[78\] Chief Delphi otomatik gönderiye izin vermiyor; Discord kapsam dışı. Vendor dokümanlarının yeniden dağıtımı (LLM'e tam metin sunmak) için her vendor'ın lisansını ayrıca kontrol edin — alıntı/özet + bağlantı en güvenli yaklaşım.

## Sources

1. [/posts.json API endpoint blocked by Cloudflare](https://www.chiefdelphi.com/t/posts-json-api-endpoint-blocked-by-cloudflare/470160)
2. [GitHub - wpilibsuite/vendor-json-repo: WPILib Vendor JSON Repository · GitHub](https://github.com/wpilibsuite/vendor-json-repo)
3. [vendor-json-repo/README.md at main · wpilibsuite/vendor-json-repo](https://github.com/wpilibsuite/vendor-json-repo/blob/main/README.md)
4. [DCMotor (WPILib API 2026.2.2)](https://github.wpilib.org/allwpilib/docs/release/java/edu/wpi/first/math/system/plant/DCMotor.html)
5. [\[WPILib Blog\] 2026 Kickoff Release of WPILib - Programming - Chief Delphi](https://www.chiefdelphi.com/t/wpilib-blog-2026-kickoff-release-of-wpilib/510119)
6. [2026 Kickoff Release of WPILib — WPILib](https://wpilib.org/blog/2026-kickoff-release-of-wpilib)
7. [WPILib 2026.2.1 Update Release - Programming - Chief Delphi](https://www.chiefdelphi.com/t/wpilib-2026-2-1-update-release/511841)
8. [wpilibsuite/allwpilib v2027.0.0-alpha-7 on GitHub](https://newreleases.io/project/github/wpilibsuite/allwpilib/release/v2027.0.0-alpha-7)
9. [SystemcoreTesting/README.md at main · wpilibsuite/SystemcoreTesting](https://github.com/wpilibsuite/SystemcoreTesting/blob/main/README.md)
10. [WPILibC++: frc/system/plant/DCMotor.h Source File](https://github.wpilib.org/allwpilib/docs/release/cpp/_d_c_motor_8h_source.html)
11. [FRC Motor Comparison & Specs | ReCalc](https://www.reca.lc/motors)
12. [ReduxLib Docs and Installation | REDUX](https://docs.reduxrobotics.com/reduxlib)
13. [\[WPILib Blog\] 2026 Control System Beta Testing - General Forum - Chief Delphi](https://www.chiefdelphi.com/t/wpilib-blog-2026-control-system-beta-testing/508770)
14. [allwpilib/MavenArtifacts.md at main · wpilibsuite/allwpilib](https://github.com/wpilibsuite/allwpilib/blob/main/MavenArtifacts.md)
15. [Index of release/edu/wpi/first/wpilibj/templates](https://www.kauailabs.com/maven2/edu/wpi/first/wpilibj/templates/)
16. [3rd Party Libraries — FIRST Robotics Competition documentation](https://docs.wpilib.org/en/stable/docs/software/vscode-overview/3rd-party-libraries.html)
17. [3rd Party Libraries — FIRST Robotics Competition documentation](https://docs.wpilib.org/en/2023_a/docs/software/vscode-overview/3rd-party-libraries.html)
18. [The FIRST Robotics Competition Blog Has Moved! | FIRST](https://www.firstinspires.org/robotics/frc/blog/2024-the-first-robotics-competition-blog-has-moved)
19. [It’s Coming! And More! | FIRST](https://www.firstinspires.org/robotics/frc/blog/its-coming-and-more)
20. [FRC Events API | Api Directory](https://apis.guru/apis/firstinspires.org)
21. [Mockoon - Mock sample for your project: FRC Events API](https://mockoon.com/mock-samples/firstinspiresorg/)
22. [FRC Event Web : API Information](https://frc-events.firstinspires.org/services/API)
23. [GitHub - lasarobotics/PurpleLib: Custom library for 418 Purple Haze](https://github.com/lasarobotics/PurpleLib)
24. [Dependency Installation - YAGSL - Yet Another Software Suite](https://docs.yagsl.com/configuring-yagsl/dependency-installation)
25. [Releases · CrossTheRoadElec/Phoenix-Releases](https://github.com/CrossTheRoadElec/Phoenix-Releases/releases/)
26. [Installing Phoenix 6 (FRC)](https://pro.docs.ctr-electronics.com/en/latest/docs/installation/installation-frc.html)
27. [Phoenix6-Documentation/source/docs/installation/installation-frc.rst at main · CrossTheRoadElec/Phoenix6-Documentation](https://github.com/CrossTheRoadElec/Phoenix6-Documentation/blob/main/source/docs/installation/installation-frc.rst)
28. [New for 2026 - Phoenix 6 Documentation - CTR Electronics](https://v6.docs.ctr-electronics.com/en/latest/docs/yearly-changes/yearly-changelog.html)
29. <https://yagsl.yassrobotics.com/>
30. [Installation | REVLib | REV Robotics Documentation](https://docs.revrobotics.com/revlib/install)
31. [REV Robotics · GitHub](https://github.com/revrobotics)
32. [📝 Unofficial REV-Compatible Logger | AdvantageScope](https://docs.advantagescope.org/more-features/urcl/)
33. [SystemCore: FRC's New 2027 Control System Explained · LearnFRC](https://learnfrc.com/blog/frc-systemcore)
34. [Canandmag Overview | REDUX](https://docs.reduxrobotics.com/canandmag/)
35. [Redux Robotics | REDUX](https://docs.reduxrobotics.com/)
36. [YAGSL-gitbook/configuring-yagsl/dependency-installation.md at main · thenetworkgrinch/YAGSL-gitbook](https://github.com/thenetworkgrinch/YAGSL-gitbook/blob/main/configuring-yagsl/dependency-installation.md)
37. [GitHub - Studica-Robotics/NavX: Repo for finding binaries and issue tracking · GitHub](https://github.com/Studica-Robotics/NavX)
38. [FRC Software library Installation](https://www.playingwithfusion.com/docview.php?docid=1205)
39. [LaserCAN – Grapple Robotics](https://grapplerobotics.au/product/lasercan/)
40. [GitHub - GrappleRobotics/libgrapplefrc: Grapple's FRC Vendor Library](https://github.com/GrappleRobotics/libgrapplefrc)
41. [Announcing the LaserCAN from Grapple Robotics - Page 8 - Electrical - Chief Delphi](https://www.chiefdelphi.com/t/announcing-the-lasercan-from-grapple-robotics/446442?page=8)
42. [YAGSL/vendordep/vendordeps/ThriftyLib-2026.0.0.json at main · Yet-Another-Software-Suite/YAGSL](https://github.com/Yet-Another-Software-Suite/YAGSL/blob/main/vendordep/vendordeps/ThriftyLib-2026.0.0.json)
43. [Workflow runs · wpilibsuite/vendor-json-repo](https://github.com/wpilibsuite/vendor-json-repo/actions)
44. [Thrifty Bot - Nova Firmware Releases](https://docs.home.thethriftybot.com/pages/nova-firmware.html)
45. [limelightlib-wpijava/LimelightHelpers.java at main · LimelightVision/limelightlib-wpijava](https://github.com/LimelightVision/limelightlib-wpijava/blob/main/LimelightHelpers.java)
46. [FRC Programming with LimelightLib (WPILib Java & C++) | Limelight Documentation](https://docs.limelightvision.io/docs/docs-limelight/apis/limelight-lib)
47. [Split vision back into two lessons, on the LimelightLib 2 vendordep by JosephTLockwood · Pull Request #118 · Hemlock5712/Workshop-Site](https://github.com/Hemlock5712/Workshop-Site/pull/118)
48. [GitHub - LimelightVision/limelightlib-wpijava · GitHub](https://github.com/LimelightVision/limelightlib-wpijava)
49. [Limelightlib C++: LimelightHelpers Namespace Reference](https://limelightlib-wpicpp-reference.limelightvision.io/namespaceLimelightHelpers.html)
50. [2027 Control System: Systemcore and WPILib | FIRST Mentor Conference](https://firstmentorconference.com/2027-control-system-systemcore-and-wpilib/)
51. [GitHub - Yet-Another-Software-Suite/YALL: YALL is an improved version of the LimelightHelpers script released by LimelightVision. · GitHub](https://github.com/Yet-Another-Software-Suite/YALL)
52. [Releases · PhotonVision/photonvision](https://github.com/PhotonVision/photonvision/releases)
53. [photonvision-docs/source/docs/programming/photonlib/adding-vendordep.rst at master · PhotonVision/photonvision-docs](https://github.com/PhotonVision/photonvision-docs/blob/master/source/docs/programming/photonlib/adding-vendordep.rst)
54. [Need help with YAGSL for 2026 project - FIRST - Chief Delphi](https://www.chiefdelphi.com/t/need-help-with-yagsl-for-2026-project/510584)
55. [Getting Started | PathPlanner Docs](https://pathplanner.dev/pplib-getting-started.html)
56. [GitHub - TexasTorque/TorqueVendordeps: Common up-to-date vendordeps. · GitHub](https://github.com/TexasTorque/TorqueVendordeps)
57. [Getting Started - Choreo Documentation](https://choreo.autos/choreolib/getting-started/)
58. [sleipnirgroup-choreolib · PyPI](https://pypi.org/project/sleipnirgroup-choreolib/)
59. [Migrate to Commands v3 (blocked by #30: ChoreoLib is v2-only) · Issue #31 · TripleHelixProgramming/Biocore](https://github.com/TripleHelixProgramming/Biocore/issues/31)
60. [Existing Projects | AdvantageKit](https://docs.advantagekit.org/getting-started/installation/existing-projects/)
61. [SystemcoreTesting/AdvantageKit.md at main · wpilibsuite/SystemcoreTesting](https://github.com/wpilibsuite/SystemCoreTesting/blob/main/AdvantageKit.md)
62. [Installation - MapleSim](https://shenzhen-robotics-alliance.github.io/maple-sim/installing-maple-sim/)
63. [Releases · Shenzhen-Robotics-Alliance/maple-sim](https://github.com/Shenzhen-Robotics-Alliance/maple-sim/releases)
64. [MapleSim](https://shenzhen-robotics-alliance.github.io/maple-sim/)
65. [Releases · wpilibsuite/allwpilib](https://github.com/wpilibsuite/allwpilib/releases)
66. [DCMotor — RobotPy API documentation](https://robotpy.readthedocs.io/projects/robotpy/en/stable/wpimath.system.plant/DCMotor.html)
67. [GitHub - tervay/recalc: A collaboration focused mechanical design calculator, primarily for FRC · GitHub](https://github.com/tervay/recalc)
68. [GitHub - rylero/chiefdelphi-mcp: Read-only MCP server that treats Chief Delphi as a local FRC design knowledge base · GitHub](https://github.com/rylero/chiefdelphi-mcp)
69. [Developer APIs - The Blue Alliance](https://www.thebluealliance.com/apidocs)
70. [The Blue Alliance - API Docs](https://py2.thebluealliance.com/apidocs/v2)
71. [Data, TBA, and You – The Blue Alliance Blog](https://blog.thebluealliance.com/2019/05/21/2019-data-dumps-and-api-information/)
72. [The Blue Alliance - Webhooks](https://www.thebluealliance.com/apidocs/webhooks)
73. [Statbotics REST API 3.0.0 OAS 3.1](https://www.statbotics.io/docs/rest)
74. [statbotics 3.0.0 on PyPI - Libraries.io - security & maintenance data for open source software](https://libraries.io/pypi/statbotics)
75. [avgupta456/statbotics | DeepWiki](https://deepwiki.com/avgupta456/statbotics)
76. [Phoenix 6 Documentation - CTR Electronics](https://v6.docs.ctr-electronics.com/_/downloads/en/latest/pdf/)
77. [FTC Events Api](https://ftc-events.firstinspires.org/api-docs/index.html)
78. [FTC Event Web : API Information](https://ftc-events.firstinspires.org/services/API)
