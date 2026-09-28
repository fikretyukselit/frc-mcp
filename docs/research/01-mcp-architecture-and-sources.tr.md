# **FIRST Robotics Competition (FRC) Ekosistemi için Model Context Protocol Mimarisi ve Dinamik Veri Kaynakları**

Modern FIRST Robotics Competition (FRC) mühendislik ekosistemi; gömülü kontrol sistemleri, dağıtık mikrodenetleyici ağları, gerçek zamanlı yol optimizasyonu ve deterministik simülasyon katmanlarını içeren yüksek karmaşıklıkta bir robotik yazılım mimarisine dayanmaktadır1. Bu ekosistem içerisinde otonom kod üretimi, şasi yapılandırması ve donanım soyutlaması gerçekleştirecek Go tabanlı bir Model Context Protocol (MCP) sunucusunun tasarımı; resmî kütüphanelerden üçüncü taraf satıcı bağımlılıklarına (vendordep), teknik bültenlerden dinamik kural motorlarına kadar uzanan heterojen veri kaynaklarının düzenli aralıklarla çekilmesini (fetch), doğrulanmasını ve anlamsal olarak indekslenmesini zorunlu kılmaktadır4.

## **Kontrol Sistemi Omurgası ve Temel Kütüphaneler (WPILib)**

FRC yazılım omurgasının merkezinde, Worcester Polytechnic Institute tarafından koordine edilen ve FIRST organizasyonunun resmî standart kabul ettiği WPILib yer almaktadır4. Robot kontrol döngüleri, sensör ve aktüatör donanım soyutlama katmanları (HAL), durum makineleri ve NetworkTables v4 telemetri protokolü doğrudan WPILib API sözleşmeleri üzerinden yürütülmektedir4. WPILib projeleri, Java ve C++ ortamlarında derleme, bağımlılık çözme ve robot kontrolcüsüne yükleme işlemlerini GradleRIO isimli Gradle eklentisi vasıtasıyla gerçekleştirmektedir5.  
FRC kontrol mimarisi, uzun yıllardır endüstri standardı olan National Instruments roboRIO ve roboRIO 2.0 platformlarından, daha yüksek hesaplama kapasitesine ve çağdaş bir Linux gerçek zamanlı çekirdeğine sahip Systemcore mimarisine doğru köklü bir geçiş sürecindedir6. Bu dönüşüm süreci, kontrol yazılımında çok sayıda yapısal kırılmaya yol açmaktadır8. Özellikle zaman damgası çözünürlüklerinin mikrosaniyeden nanosaniyeye çekilmesi, çoklu motor gruplarını yöneten MotorControllerGroup yapısının kaldırılarak yerine doğrudan modern kontrolcü hiyerarşilerinin getirilmesi ve DriverStation sınıfının MatchState ile RobotState gibi özelleşmiş alt bileşenlere ayrılması gibi majör API değişiklikleri, kod üretim motorlarının hedef sezonu dinamik olarak tanımasını zorunlu kılmaktadır8. Bir MCP sunucusu, geliştiricinin çalıştığı sezon versiyonuna göre API sözleşmelerini doğrulamak amacıyla WPILib dokümantasyonunu ve GitHub Releases meta verilerini sürekli olarak izlemelidir4.

| Kaynak Bileşeni | Protokol ve Veri Tipi | Uç Nokta (URL / Kaynak Adresi) | MCP İzleme Stratejisi ve İçerik Kapsamı |
| :---- | :---- | :---- | :---- |
| **WPILib Resmî Dokümantasyonu (Stable)** | HTML / Sphinx Web Dokümanı | https://docs.wpilib.org/en/stable/ \[cite: 4, 5\] | HTTP GET ve metin indeksleme; komut tabanlı çerçeve ve kinematik API kılavuzları4. |
| **WPILib Geliştirici Dokümantasyonu (Latest)** | HTML / Sphinx Web Dokümanı | https://docs.wpilib.org/en/latest/ \[cite: 11\] | HTTP GET; Systemcore ve yeni sezon önizleme dokümanları8. |
| **AllWPILib Kaynak Kodu ve Sürümler** | REST API (GitHub Releases) | api.github.com/repos/wpilibsuite/allwpilib/releases \[cite: 11\] | ETag doğrulamalı JSON sorgusu; derleme bağımlılıkları ve changelog ayrıştırma11. |
| **GradleRIO Derleme Altyapısı** | REST API (GitHub Releases) | api.github.com/repos/wpilibsuite/GradleRIO/releases \[cite: 5\] | GitHub Releases takibi; Maven araç zinciri ve derleme bayrakları yönetimi5. |
| **WPILib Yıllık Sürüm Değişiklikleri** | Web / Markdown Metni | docs.wpilib.org/en/latest/docs/yearly-overview/yearly-changelog.html \[cite: 8\] | Metin farkı (diff) analizi; kullanım dışı bırakılan ve eklenen API listesi8. |

## **Aktüatör Sistemleri, Akıllı Motor Kontrolcüleri ve Sensör Bağımlılıkları (Vendordeps)**

Modern FRC robotlarında yüksek güç yoğunluğuna sahip fırçasız (brushless) motorlar ve akıllı motor kontrolcüleri kullanılmaktadır1. Bu bileşenler geleneksel darbe genişlik modülasyonu (PWM) yerine Controller Area Network (CAN) veri yolu üzerinden üreticiye özel protokollerle denetlenmektedir4. Üreticiler, kendi donanımlarının kontrol kütüphanelerini robot projelerine vendordep adı verilen JSON şemaları aracılığıyla sunmaktadır4. Bir MCP sunucusu, üreticilerin her yıl güncellediği bu JSON tanımlarını dinamik olarak indirip robot derleme ortamına enjekte edebilmelidir4.  
Cross The Road Electronics (CTRE) ekosistemi, güncel robot tasarımında yaygın olarak kullanılan Kraken X60 ve Falcon 500 fırçasız motorlarını, Talon FX motor kontrolcülerini, CANcoder mutlak açı enkoderlerini ve Pigeon 2 ataletsel ölçüm birimlerini (IMU) Phoenix 6 API çerçevesi altında birleştirmektedir1. Phoenix 6 mimarisi; doğrudan kontrolcü donanımı üzerinde çalışan Motion Magic hareket profillemesi, Fused CANcoder üzerinden kapalı çevrim pozisyon kontrolü, tork akımı denetimi (TorqueCurrentFOC) ve 250 Hz'e varan CAN-FD durum sinyali (Status Signals) yayınlama kabiliyeti sunmaktadır1. CTRE, bellenim (firmware), Tuner X masaüstü yapılandırma yazılımı ve API güncellemelerini düzenli olarak resmî bir RSS akışı üzerinden duyurmaktadır1.  
REV Robotics ekosistemi ise NEO ve NEO Vortex fırçasız motorlarını süren SPARK MAX ve SPARK Flex kontrolcülerini kapsamaktadır12. SPARK kontrolcüleri, cihaz üzerinde kalıcı konfigürasyon depolama, yerleşik Hall sensörü okuma ve Through Bore Encoder gibi harici mutlak enkoderleri alternatif geri besleme kaynağı olarak kullanma imkânı tanımaktadır12. REV, kütüphane dağıtımlarını CloudFront tabanlı bir meta veri sunucusu üzerinden versiyonlanmış JSON dosyaları olarak servis etmektedir12. Bu iki ana üreticiye ek olarak, şasinin yönelimini 200 Hz hızında takip eden Kauai Labs / Studica navX IMU kütüphaneleri, optik mesafe tespiti sağlayan Playing With Fusion Time-of-Flight sensörleri ve Redux Robotics Canandcoder bileşenleri de standart vendordep mekanizmasıyla projeye eklenmektedir4.

| Üretici ve Ekosistem | Kapsanan Donanımlar | Resmî Vendordep JSON URL | Dokümantasyon ve Haberleşme Kanalları |
| :---- | :---- | :---- | :---- |
| **CTRE Phoenix 6** | Talon FX, Kraken X60, Pigeon 2, CANcoder1 | maven.ctr-electronics.com/release/com/ctre/phoenix6/latest/Phoenix6-frc2025-latest.json | v6.docs.ctr-electronics.com; RSS: api.ctr-electronics.com/rss/rss.xml \[cite: 1\] |
| **REV Robotics (REVLib)** | SPARK MAX, SPARK Flex, NEO, NEO Vortex12 | software-metadata.revrobotics.com/REVLib-2026.json12 *(2027: REVLib-2027.json)*21 | docs.revrobotics.com/revlib \[cite: 12\] |
| **Studica / Kauai Labs** | navX-MXP, navX2-Micro, Sensör Füzyonu13 | dev.studica.com/releases/2024/NavX.json | pdocs.kauailabs.com/navx-mxp |
| **Playing With Fusion** | Time-of-Flight Lazer Mesafe, CAN Termokupl4 | playingwithfusion.com/frc/playingwithfusion.json | playingwithfusion.com/doc \[cite: 18\] |
| **Redux Robotics** | Canandcoder, Canandgyro, Canandmag20 | apidocs.reduxrobotics.com/current/ReduxLib.json | apidocs.reduxrobotics.com |

## **Kinematik, Şasi Dinamiği ve Otonom Yol Planlama**

FRC robotlarında holonomik hareket kabiliyeti sağlayan Swerve (bağımsız yönlendirilebilir ve sürülebilir tekerlek) mekanizmaları rekabet standardı haline gelmiştir2. Bir swerve şasinin fiziksel olarak doğru yönetilebilmesi; kinematik dönüşümler, modül açısı optimizasyonu (inversion avoidance), tekerlek hız doygunluğu (desaturation) ve kapalı çevrim odometri algoritmalarının eşzamanlı çalışmasını gerektirir16.  
Yet Another Generic Swerve Library (YAGSL), farklı donanım kombinasyonlarını soyutlamak amacıyla geliştirilmiş açık kaynaklı bir kütüphanedir20. YAGSL sayesinde sürüş motoru bir SPARK MAX, yön motoru bir Talon FX ve mutlak enkoderi bir CANcoder olan karmaşık donanım düzenleri tek bir çatı altında kontrol edilebilmektedir16. Kütüphane, tüm fiziksel şasi ölçülerini, dişli redüksiyon oranlarını, akım limitlerini ve PID kazançlarını robot projesinin deploy/swerve/ klasörü altındaki JSON dosyalarından çalışma zamanında dinamik olarak okur16. Ayrıca ağırlık merkezine bağlı olarak devrilmeyi engelleyen momentum sınırlandırma algoritmaları ve 20 milisaniyelik bağımsız odometri iş parçacığı barındırır16.  
Otonom periyotta şasinin saha üzerindeki rotasını hassas bir şekilde takip edebilmesi için görsel yol planlayıcılar kullanılmaktadır2. PathPlanner, holonomik ve diferansiyel sürüşler için Bézier eğrileri tabanlı hareket profilleri üreten bir masaüstü yazılımı ve bu profilleri robot üzerinde yürüten PathPlannerLib robot kütüphanesinden oluşur22. Robot kodunu yeniden derlemeye gerek kalmadan rotaların güncellenmesini sağlayan sıcak yeniden yükleme (hot reload), rota üzerinde mekanizmaları tetikleyen olay işaretleyicileri (event markers) ve AD\* algoritmasıyla sahadaki dinamik engellerin etrafından dolaşmayı sağlayan yol bulma (pathfinding) yetenekleri bu mimarinin temel parçalarıdır22.  
Yörünge planlama alanındaki bir diğer yenilik olan Choreo, hareket profilini fiziksel motor sınırları, şasi eylemsizlik momentleri ve tekerlek sürtünme katsayılarına göre çözen bir doğrusal olmayan optimizasyon (non-linear optimization / collocation) aracıdır24. Ürettiği zaman-optimal (time-optimal) yörünge çıktıları ChoreoLib vasıtasıyla robot koduna aktarılmaktadır4.

| Kütüphane ve Araç | Mimari Kapsam ve Görev | Vendordep JSON URL | Dokümantasyon ve Depo |
| :---- | :---- | :---- | :---- |
| **YAGSL (Vendor Lib)** | Donanım Bağımsız Swerve Şasi Soyutlaması20 | https://broncbotz3481.github.io/YAGSL-Lib/yagsl/yagsl.json \[cite: 16, 20\] | github.com/BroncBotz3481/YAGSL-Lib \[cite: 25\] |
| **PathPlannerLib** | Bézier Eğrili Otonom Yol ve Rota Takibi22 | https://3015rangerrobotics.github.io/pathplannerlib/PathplannerLib.json \[cite: 22, 23\] | pathplanner.dev \[cite: 22, 23\] |
| **ChoreoLib** | Zaman-Optimal Yörünge Optimizasyonu24 | https://choreo.autos/vendordeps/ChoreoLib.json \[cite: 24\] | choreo.autos \[cite: 24\] |

## **Görsel Konumlandırma, AprilTag Algılama ve Ataletsel Odometri**

Tekerlek enkoderleri ve jiroskop tabanlı klasik odometri sistemleri, maç esnasında tekerleklerin kayması ve robot çarpışmaları sebebiyle zamanla kümülatif konum hataları üretir16. Bu hatayı gidermek amacıyla FRC sahalarında AprilTag adı verilen iki boyutlu optik etiketler kullanılmaktadır26. Robotlar, sahadaki AprilTag etiketlerini tespit ederek kamera ile etiket arasındaki 3D dönüşüm matrisini çözer ve elde edilen saha içi mutlak koordinatı genişletilmiş Kalman filtresi (WPILib Pose Estimator) üzerinden odometriye dahil eder26.  
PhotonVision, Raspberry Pi 4/5 veya Orange Pi 5 gibi tek kart bilgisayarlarda koşan açık kaynaklı bir görüntü işleme platformudur27. Çoklu kamera girişlerini işleyerek 3D etiket pozisyonlarını hesaplar ve robot kontrolcüsüne NetworkTables veya doğrudan C++/Java API kütüphanesi (PhotonLib) üzerinden iletir27. PhotonVision sürümleri, donanım platformlarına özel bellenim imajları ve Maven kütüphane meta verileri şeklinde GitHub Releases üzerinden dağıtılmaktadır27.  
Limelight platformu ise kamera sensörünü, işlemci kartını ve ayarlanabilir aydınlatma modüllerini tek bir endüstriyel gövdede birleştiren akıllı bir vizyon donanımıdır9. Limelight, MegaTag ve MegaTag2 algoritmaları vasıtasıyla dahili IMU verisini AprilTag gözlemleriyle donanım seviyesinde harmanlayarak yüksek frekansta konum kestirimi sağlar9. Yazılımcılar bu verileri doğrudan LimelightHelpers.java dosyası üzerinden projelerine bağlayabildikleri gibi, topluluk tarafından geliştirilen YALL (Yet Another Limelight Library) vendordep altyapısını da kullanabilmektedir28.  
Görsel takip mimarisindeki en güncel gelişmelerden biri, artırılmış gerçeklik gözlüklerinin dahili görsel ataletsel odometri (VIO) algoritmalarını robot şasisine taşıyan QuestNav projesidir26. Robot şasisine monte edilen donanım üzerinden mutlak sürüklenme yapmayan uzaysal takip verisi üretilir ve questnavlib.json aracılığıyla robot kontrol döngüsüne aktarılır26.

| Görsel Konumlandırma Sistemi | Donanım ve Yazılım Tipi | Bağımlılık URL / Entegrasyon Modeli | Dokümantasyon ve Kaynak Depoları |
| :---- | :---- | :---- | :---- |
| **PhotonVision (PhotonLib)** | Açık Kaynak Görsel Algılama Yazılımı27 | maven.photonvision.org/repository/internal/org/photonvision/PhotonLib-json/1.0/PhotonLib-json-1.0.json \[cite: 29, 30\] | docs.photonvision.org13; GitHub: PhotonVision/photonvision27 |
| **Limelight Helpers** | Entegre Akıllı Kamera Donanımı9 | Proje kaynak koduna eklenen LimelightHelpers.java \[cite: 28\] | docs.limelightvision.io \[cite: 9\] |
| **YALL (Limelight Lib)** | Limelight Vendordep Alternatifi31 | https://Yet-Another-Software-Suite.github.io/YALL/yall.json \[cite: 31\] | github.com/Yet-Another-Software-Suite/YALL \[cite: 31\] |
| **QuestNav (VIO Tracking)** | VR Donanım Tabanlı Optik Odometri26 | GitHub Releases: questnavlib.json \[cite: 26\] | questnav.gg/docs \[cite: 26\] |

## **Deterministik Telemetri, Loglama ve Simülasyon**

Geleneksel robot yazılımı geliştirme süreçlerinde hata ayıklama fiziksel robot üzerindeki deneme-yanılma testlerine dayanırken, modern FRC yazılım mühendisliği veri odaklı deterministik simülasyon ve log tekrarı (log replay) paradigmalarına yönelmiştir1.  
Team 6328 (Mechanical Advantage) tarafından geliştirilen AdvantageKit çerçevesi, robot üzerindeki tüm donanım girdilerini (sensör verileri, motor akımları, joystick komutları) her döngüde deterministik olarak kaydeder3. AdvantageKit kullanılan bir mimaride robot programı standart TimedRobot yerine LoggedRobot sınıfından türetilir3. Fiziksel donanım çalışırken tüm girdi sinyalleri yüksek hızlı .wpilog formatında USB belleğe yazılır3. Robot kodu fiziksel robot olmadan masaüstü simülasyonunda çalıştırıldığında, kaydedilen girdi kütükleri simülasyon motoruna beslenir; kontrol algoritması sahada gerçekleşen fiziksel durumun birebir kopyasını oluşturarak çıktılar üretir3. Bu süreç, mantıksal hataların fiziksel robota ihtiyaç duyulmadan saniyeler içinde tespit edilmesine olanak tanır3.  
REV motor kontrolcülerinin CAN veri trafiği standart telemetri hatlarında yüksek yük oluşturabildiğinden, bu verilerin deterministik olarak kaydedilmesi özel bir optimizasyon gerektirir33. AdvantageScope ekibi tarafından sağlanan URCL (Unofficial REV-Compatible Logger), SPARK MAX ve SPARK Flex kontrolcülerinin CAN mesajlarını düşük işlemci tüketimiyle yakalayarak AdvantageScope telemetri analiz aracına aktarmaktadır33.

| Telemetri ve Loglama Aracı | Görevi ve Çalışma Prensibi | Vendordep JSON URL | Depo ve Dokümantasyon |
| :---- | :---- | :---- | :---- |
| **AdvantageKit** | Deterministik Girdi Loglama ve Simülasyon Tekrarı3 | https://github.com/Mechanical-Advantage/AdvantageKit/releases/latest/download/AdvantageKit.json \[cite: 3\] | docs.advantagekit.org \[cite: 3\] |
| **URCL** | REV CAN Veri Çerçevesi Loglayıcı33 | https://raw.githubusercontent.com/Mechanical-Advantage/URCL/main/URCL.json \[cite: 33\] | github.com/Mechanical-Advantage/URCL \[cite: 33\] |
| **AdvantageScope** | 3D Robot ve Telemetri Görselleştirme Yazılımı2 | Masaüstü Uygulaması (GitHub Dağıtımı) | docs.advantagescope.org \[cite: 33\] |

## **Resmî Yarışma Kuralları, Bloglar, Q\&A ve Topluluk Veri Akışları**

Otonom kod üreten ve şasi parametrelerini optimize eden bir MCP sunucusu, yalnızca teknik kütüphaneleri değil, aynı zamanda robotun yarışma kurallarına uygunluğunu belirleyen idari ve regülatif kısıtlamaları da doğrulamak zorundadır34. Sezon boyunca robot ağırlığı, başlangıç hacmi, motor sınırlamaları ve otonom periyot kuralları sürekli olarak güncellenir34.  
FIRST operasyon direktörlüğü tarafından yayınlanan resmî duyurular, kural değişiklikleri ve Systemcore gibi kontrol sistemi dönüşüm takvimleri, FIRST Community Blog üzerinden duyurulmaktadır6. Yarışma sezonu boyunca (Ocak ayı başından Şampiyona dönemine kadar) her Salı ve Cuma günü "Team Updates" (Takım Güncellemeleri) başlığı altında resmî kural değişiklikleri yayınlanmakta ve Game Manual belgeleri revize edilmektedir35. Oyun tasarım komitesi ve baş hakemlerin kural yorumlamalarını bağlayıcı kararlarla sunduğu resmî FRC Soru-Cevap (Q\&A) platformu ise tüm yeni cevapları içeren resmî bir RSS beslemesine sahiptir36.  
Topluluk düzeyinde ise Chief Delphi forumu, takımların açık paylaşımlar yaptığı (Open Alliance), mekanik tasarımların ve yazılım mimarilerinin tartışıldığı ana bilgi merkezidir38. Chief Delphi platformunun Discourse altyapısı, yazılım ve kontrol kategorileri için yerleşik RSS akışları sunmaktadır. Yarışma etkinlikleri, takım eşleşmeleri, dünya sıralamaları ve maç bazlı OPR (Offensive Power Rating) istatistikleri için The Blue Alliance (TBA) REST API v3 uç noktaları en güvenilir veri sağlayıcısı konumundadır.

| Bilgi Kaynağı | Protokol / Format | URL / Veri Uç Noktası | İçerik ve MCP Takip Amacı |
| :---- | :---- | :---- | :---- |
| **FIRST Community Blog (FRC)** | HTML / Web Duyuruları | https://community.firstinspires.org/topic/frc \[cite: 6\] | Sezon takvimi, idari duyurular ve kural önizlemeleri6. |
| **Resmî FRC Q\&A Sistemi** | RSS / XML Beslemesi | https://frc-qa.firstinspires.org/answers.rss \[cite: 36\] | Hakem heyeti bağlayıcı kural yorumlamaları ve kararları36. |
| **Chief Delphi (Programming)** | RSS / XML Beslemesi | https://www.chiefdelphi.com/c/technical/programming/11.rss | Topluluk yazılım tartışmaları, kütüphane hataları ve algoritmalar. |
| **Chief Delphi (Control Systems)** | RSS / XML Beslemesi | https://www.chiefdelphi.com/c/technical/control-systems/9.rss | CAN veri yolu, kablolama, bellenim ve motor sürücü tartışmaları. |
| **The Blue Alliance (TBA)** | REST API v3 (JSON) | https://www.thebluealliance.com/api/v3/ | Maç sonuçları, ittifak performansları ve etkinlik verileri. |
| **CTRE Engineering Changelog** | RSS / XML Beslemesi | https://api.ctr-electronics.com/rss/rss.xml \[cite: 1\] | Talon FX bellenimi, Phoenix API ve Tuner X bültenleri1. |

## **Go Tabanlı Robot Builder MCP Sunucusu Mimari Tasarımı**

Model Context Protocol (MCP), büyük dil modellerinin (LLM) geliştirme ortamındaki yerel ve uzak araçlarla güvenli, yapılandırılmış bir protokol üzerinden konuşmasını sağlayan bir açık standarttır. Go dili; düşük bellek ayak izi, yüksek performanslı yerleşik HTTP/ağ kütüphaneleri ve Goroutine tabanlı eşzamanlılık (concurrency) mekanizmaları sayesinde bu MCP sunucusunun inşası için ideal bir zemin sunar.

### **Arka Plan Veri Çekme Motoru (Background Polling Engine)**

Go sunucusu, harici servisleri bloklamayan bağımsız time.Ticker döngüleri ve eşzamanlı çalışan Goroutine havuzları ile veri çekme süreçlerini yürütmelidir. Dış sunuculara gereksiz bant genişliği yüklememek ve GitHub REST API hız sınırlarına (rate limits) takılmamak amacıyla yapılan HTTP isteklerinde If-None-Match (HTTP ETag) ve If-Modified-Since başlıkları aktif olarak yönetilmelidir. Uzak sunucu 304 Not Modified yanıtı döndüğünde ayrıştırma adımı atlanarak yerel önbellek korunur; 200 OK alındığında ise yeni yük yerel veri deposuna aktarılır.  
Veri çekme mekanizması üç farklı zamanlama döngüsüne ayrılmalıdır. Birinci döngü, 15 ila 30 dakikalık periyotlarla çalışan yüksek öncelikli RSS dinleyicisidir. Bu döngü CTRE donanım RSS'i1, FRC Q\&A yanıt akışı36 ve Chief Delphi yazılım kategorisini tarayarak yeni kural veya bellenim duyurularını anında yakalar. İkinci döngü, günlük olarak çalışan vendordep ve sürüm denetleyicisidir. REV12, CTRE1, PathPlanner22, YAGSL16 ve AdvantageKit3 JSON dosyalarını indirerek şemalardaki versiyon numaralarını karşılaştırır. Üçüncü döngü ise haftalık çalışan dokümantasyon senkronizasyonudur; WPILib4, YAGSL ve PathPlanner doküman sayfalarını tarayarak yerel gömülü arama motoru (örneğin Bleve veya SQLite FTS5) için metin parçalarına (chunks) dönüştürür.

### **MCP Protokol Bileşenleri (Resources, Tools, Prompts)**

Sunucu, istemci yapay zekâ modeline üç temel MCP nesnesi sağlar:

> * **Resources (Kaynaklar):** Proje bağlamına doğrudan eklenebilen statik ve dinamik veri şemalarıdır. Örneğin frc://vendordeps/{vendor}/{season} URI şeması çağrıldığında sunucu, ilgili üreticinin en güncel vendordep JSON dosyasını döndürür4. frc://swerve/yagsl-template kaynağı, YAGSL şasi konfigürasyonu için gereken chassis.json, modules/{fl,fr,bl,br}.json ve physicalproperties.json şablonlarını sunar16.  
> * **Tools (Araçlar):** LLM'in otonom olarak çağırabileceği işlevsel fonksiyonlardır. Bu araçlar; dokümantasyon sorgulama, kural kitabı doğrulama ve kod oluşturma parametrelerini hesaplama görevlerini üstlenir.  
> * **Prompts (Hazır İstemler):** "Yeni bir swerve alt sistemi oluştur", "PathPlanner otonom komut zinciri bağla" veya "AprilTag odometri füzyonu ekle" gibi karmaşık görevleri başlatan parametrik şablonlardır2.

| MCP Aracı (Tool Adı) | Girdi Parametreleri (JSON Schema) | Çıktı ve Görev Tanımı |
| :---- | :---- | :---- |
| search\_frc\_docs | query (string), domain (enum: wpilib, yagsl, pathplanner, ctre, rev) | Yerel metin indeksinde arama yaparak ilgili sınıfların kullanım örneklerini ve doküman paragraflarını döner4. |
| resolve\_vendordep | vendor\_id (string), season (int) | En güncel vendordep JSON dosyasını çözer, bağımlılık çakışmalarını denetler ve projeye doğrudan eklenebilir JSON metnini üretir4. |
| generate\_yagsl\_config | chassis\_width, chassis\_length, drive\_motor, steer\_motor, gyro\_type, drive\_gear\_ratio | Verilen fiziksel donanım parametrelerine uygun eksiksiz YAGSL deploy/swerve/ JSON dosya setini üretir16. |
| query\_rules\_and\_qa | keyword (string), section (string) | Game Manual kural maddelerini ve FRC Q\&A yanıtlarını tarayarak robot mekanizmasının kurala uygunluğunu doğrular35. |
| check\_hardware\_firmware | device\_family (enum: ctre, rev, limelight) | Son yayınlanan bellenim (firmware) sürümlerini ve bilinen kritik donanım hatalarını listeler1. |

## **Sonuç ve Mimari Yol Haritası**

Bir FRC robot projesinin başarıya ulaşması; donanım haberleşmesi, şasi kinematiği, optik konumlandırma ve kural uygunluğu katmanlarının hatasız entegre edilmesine bağlıdır2. Go dili ile inşa edilecek bir FRC Robot Builder MCP sunucusu; üreticilerin JSON vendordep uç noktalarını4, dokümantasyon ağlarını4 ve resmî RSS kanallarını1 düzenli periyotlarla dinleyerek yapay zekâ destekli kodlama süreçlerindeki en büyük problem olan bilgi eskimesi (hallucination / outdated API) sorununu tamamen ortadan kaldırma potansiyeline sahiptir.  
Geliştirilecek mimarinin ilk aşamasında, WPILib'in Systemcore donanımına geçişi sırasında değişen API yapılarının ve nanosaniye seviyesindeki zamanlama sözleşmelerinin Go veri tabanına işlenmesi önerilmektedir8. Eşzamanlı olarak, YAGSL ve PathPlanner konfigürasyon şemalarının MCP araçları (tools) olarak modellenmesi, robot yazılımcılarına saniyeler içinde hatasız şasi ve otonom kod iskeleleri kurma imkânı tanıyacaktır20. Bu entegre yaklaşım, yazılım ekiplerinin düşük seviyeli bağımlılık yapılandırmalarıyla vakit kaybetmesini engelleyerek doğrudan üst seviye strateji ve kontrol algoritmalarına odaklanmalarını sağlayacaktır.

#### **Works cited**

> 1. New for 2025 \- Phoenix 6 Documentation \- CTR Electronics, [https://v6.docs.ctr-electronics.com/en/2025/docs/yearly-changes/yearly-changelog.html](https://v6.docs.ctr-electronics.com/en/2025/docs/yearly-changes/yearly-changelog.html)  
> 2. GitHub \- Coconuts2486-FRC/FRC-2026: FIRST Robotics, [https://github.com/Coconuts2486-FRC/FRC-2026](https://github.com/Coconuts2486-FRC/FRC-2026)  
> 3. Existing Projects \- AdvantageKit, [https://docs.advantagekit.org/getting-started/installation/existing-projects/](https://docs.advantagekit.org/getting-started/installation/existing-projects/)  
> 4. 3rd Party Libraries \- FIRST Robotics Competition \- WPILib, [https://docs.wpilib.org/en/stable/docs/software/vscode-overview/3rd-party-libraries.html](https://docs.wpilib.org/en/stable/docs/software/vscode-overview/3rd-party-libraries.html)  
> 5. GitHub \- wpilibsuite/GradleRIO: The official gradle plugin for the, [https://github.com/wpilibsuite/GradleRIO](https://github.com/wpilibsuite/GradleRIO)  
> 6. FIRST Community Blog | FRC, [https://community.firstinspires.org/topic/frc](https://community.firstinspires.org/topic/frc)  
> 7. Announcing the Experiential Robotics Platform (XRP) Kit, [https://news.sparkfun.com/7467](https://news.sparkfun.com/7467)  
> 8. New for 2027 \- FIRST Robotics Competition \- WPILib, [https://docs.wpilib.org/en/2027/docs/yearly-overview/yearly-changelog.html](https://docs.wpilib.org/en/2027/docs/yearly-overview/yearly-changelog.html)  
> 9. GitHub \- LimelightVision/systemcore-os-public, [https://github.com/LimelightVision/systemcore-os-public](https://github.com/LimelightVision/systemcore-os-public)  
> 10. RoboRIO 2.0 not imaging \- NI Community \- National Instruments, [https://forums.ni.com/t5/Discovering-and-Imaging-the/RoboRIO-2-0-not-imaging/td-p/4185681](https://forums.ni.com/t5/Discovering-and-Imaging-the/RoboRIO-2-0-not-imaging/td-p/4185681)  
> 11. Releases · wpilibsuite/allwpilib \- GitHub, [https://github.com/wpilibsuite/allwpilib/releases](https://github.com/wpilibsuite/allwpilib/releases)  
> 12. Installation | REVLib \- REV Robotics Documentation, [https://docs.revrobotics.com/revlib/install](https://docs.revrobotics.com/revlib/install)  
> 13. Doc Ock \- FRC Team 3255's 2023 Robot \- GitHub, [https://github.com/FRCTeam3255/2023\_Robot\_Code](https://github.com/FRCTeam3255/2023_Robot_Code)  
> 14. Command-Based Programs with RobotBuilder | FirstMnCsa, [https://firstmncsa.org/2022/12/20/command-based-programs-with-robotbuilder/](https://firstmncsa.org/2022/12/20/command-based-programs-with-robotbuilder/)  
> 15. 3rd Party Libraries \- FIRST Robotics Competition \- WPILib, [https://docs.wpilib.org/en/2022/docs/software/vscode-overview/3rd-party-libraries.html](https://docs.wpilib.org/en/2022/docs/software/vscode-overview/3rd-party-libraries.html)  
> 16. Yet-Another-Software-Suite YAGSL-Example · Discussion \#29, [https://github.com/Yet-Another-Software-Suite/YAGSL-Example/discussions/29](https://github.com/Yet-Another-Software-Suite/YAGSL-Example/discussions/29)  
> 17. REVLib | REV ION Control System, [https://docs.revrobotics.com/ion-control/sw/revlib](https://docs.revrobotics.com/ion-control/sw/revlib)  
> 18. FIRST Robotics Competition documentation, [http://team2168.org/javadoc/\_readthedocs/frc-docs-stable/index.html](http://team2168.org/javadoc/_readthedocs/frc-docs-stable/index.html)  
> 19. FRC 2020 Software Development Environment \- IC Robotics, [http://icrobotics.org/cms/index.php/first/175-frc-2020-software-development-environment](http://icrobotics.org/cms/index.php/first/175-frc-2020-software-development-environment)  
> 20. Yet Another Generic Swerve Library · GitHub, [https://github.com/Yet-Another-Software-Suite/YAGSL\_old](https://github.com/Yet-Another-Software-Suite/YAGSL_old)  
> 21. SystemcoreTesting/REV.md at main \- GitHub, [https://github.com/wpilibsuite/SystemCoreTesting/blob/main/REV.md](https://github.com/wpilibsuite/SystemCoreTesting/blob/main/REV.md)  
> 22. mjansen4857/pathplanner: A simple yet powerful path ... \- GitHub, [https://github.com/mjansen4857/pathplanner](https://github.com/mjansen4857/pathplanner)  
> 23. Getting Started \- PathPlanner Docs, [https://pathplanner.dev/pplib-getting-started.html](https://pathplanner.dev/pplib-getting-started.html)  
> 24. Building ChoreoLib \- Choreo Documentation, [https://choreo.autos/contributing/building-choreolib/](https://choreo.autos/contributing/building-choreolib/)  
> 25. BroncBotz3481/YAGSL-Lib: YAGSL-Vendor Library \- GitHub, [https://github.com/BroncBotz3481/YAGSL-Lib](https://github.com/BroncBotz3481/YAGSL-Lib)  
> 26. Robot Code Setup \- QuestNav, [https://questnav.gg/docs/getting-started/robot-code/](https://questnav.gg/docs/getting-started/robot-code/)  
> 27. Releases · PhotonVision/photonvision \- GitHub, [https://github.com/PhotonVision/photonvision/releases](https://github.com/PhotonVision/photonvision/releases)  
> 28. Overture-7421/overturelib \- GitHub, [https://github.com/Overture-7421/overturelib](https://github.com/Overture-7421/overturelib)  
> 29. FRC1466/robot-code-2023 \- GitHub, [https://github.com/FRC1466/robot-code-2023](https://github.com/FRC1466/robot-code-2023)  
> 30. 3rd Party Libraries \- FIRST Robotics Competition \- WPILib, [https://docs.wpilib.org/en/2023\_a/docs/software/vscode-overview/3rd-party-libraries.html](https://docs.wpilib.org/en/2023_a/docs/software/vscode-overview/3rd-party-libraries.html)  
> 31. Yet Another Limelight Library (YALL) \- GitHub, [https://github.com/Yet-Another-Software-Suite/YALL](https://github.com/Yet-Another-Software-Suite/YALL)  
> 32. Links \- Josuah, [https://josuah.net/links/](https://josuah.net/links/)  
> 33. Unofficial REV-Compatible Logger | AdvantageScope, [https://docs.advantagescope.org/more-features/urcl/](https://docs.advantagescope.org/more-features/urcl/)  
> 34. FIRST Robotics Competition \- Wikipedia, [https://en.wikipedia.org/wiki/FIRST\_Robotics\_Competition](https://en.wikipedia.org/wiki/FIRST_Robotics_Competition)  
> 35. Post Kickoff Information \- FIRST Community Blog, [https://community.firstinspires.org/2026-post-kickoff-information](https://community.firstinspires.org/2026-post-kickoff-information)  
> 36. Question 50 \- FRC Q\&A, [https://frc-qa.firstinspires.org/qa/50](https://frc-qa.firstinspires.org/qa/50)  
> 37. Introducing a New, United FIRST Community Blog, [https://community.firstinspires.org/introducing-a-new-united-first-community-blog](https://community.firstinspires.org/introducing-a-new-united-first-community-blog)  
> 38. FRC 智库网 · FIRSTHub · FRC Open Resource Library, [https://firsthub.site/](https://firsthub.site/)