package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ============================================================
// ⚙️ CONFIGURAÇÕES
// ============================================================
const PortaGo = ":80"

var (
	db              *sql.DB
	httpClient      *http.Client
	clientAcelerado *http.Client
	streamClient    *http.Client
	CacheLock       sync.RWMutex
)

// ============================================================
// 🛡️ SISTEMA DE LOG E BLOQUEIO DE IPs
// ============================================================
const (
	arquivoIPsClientes   = "ips_clientes.txt"
	arquivoIPsBloqueados = "ips_bloqueados.txt"
)

var (
	ipsRegistrados     = make(map[string]bool)
	ipsRegistradosLock sync.RWMutex
	ipsBloqueados      = make(map[string]bool)
	ipsBloqueadosLock  sync.RWMutex
	ipsBloqueadosMod   time.Time
)

// ============================================================
// 📋 CONFIGURAÇÃO DO .ENV
// ============================================================
type CatEntry struct {
	NomeOriginal string
	NomeNovo     string
	Ordem        int
}

type EnvConfig struct {
	FormatoCanal string
	SigmaPaineis []string

	MasterLiveUser string
	MasterLivePass string
	MasterVodUser  string
	MasterVodPass  string
	M3ULive        string
	M3UVod         string
	HostLive       string
	HostVod        string

	AdminUser string
	AdminPass string
	AdminDNS  string

	FiltroSemAdultos  map[string][]string
	OrdemCanais       map[string]CatEntry
	OrdemFilmes       map[string]CatEntry
	OrdemSeries       map[string]CatEntry
	OcultaTodosCanais []string
	OcultaTodosFilmes []string
	OcultaTodosSeries []string

	WebPlayers []string
}

var Env EnvConfig

func carregarEnv() {
	Env.FormatoCanal = ".ts"
	Env.FiltroSemAdultos = make(map[string][]string)
	Env.OrdemCanais = make(map[string]CatEntry)
	Env.OrdemFilmes = make(map[string]CatEntry)
	Env.OrdemSeries = make(map[string]CatEntry)

	data, err := os.ReadFile(".env")
	if err != nil {
		fmt.Println("⚠️ .env não encontrado, usando padrões.")
		Env.SigmaPaineis = []string{"GoMaIn123"}
		return
	}

	lines := strings.Split(string(data), "\n")
	secao := ""
	ordemIdx := 0

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "========") {
			continue
		}

		lineUpper := strings.ToUpper(line)

		if strings.HasPrefix(lineUpper, "SIGMA") && strings.Contains(line, "=") {
			partes := strings.SplitN(line, "=", 2)
			if len(partes) == 2 {
				itens := strings.Split(partes[1], ",")
				for _, item := range itens {
					item = strings.TrimSpace(item)
					if item != "" {
						Env.SigmaPaineis = append(Env.SigmaPaineis, item)
					}
				}
			}
			continue
		}

		if strings.HasPrefix(lineUpper, "FORMAT_CANAL") || strings.HasPrefix(lineUpper, "FORMAT CANAL") {
			partes := strings.SplitN(line, "=", 2)
			if len(partes) == 2 {
				val := strings.TrimSpace(partes[1])
				if !strings.HasPrefix(val, ".") {
					val = "." + val
				}
				Env.FormatoCanal = val
			}
			continue
		}

		if strings.HasPrefix(lineUpper, "LIVE_HOST") || (strings.HasPrefix(lineUpper, "HOST_FONTE") && Env.HostLive == "") {
			if p := strings.SplitN(line, "=", 2); len(p) == 2 {
				Env.HostLive = strings.TrimSpace(p[1])
				if Env.HostVod == "" {
					Env.HostVod = Env.HostLive
				}
			}
			continue
		}
		if strings.HasPrefix(lineUpper, "VOD_HOST") || (strings.HasPrefix(lineUpper, "HOST_FONTE") && Env.HostLive != "") {
			if p := strings.SplitN(line, "=", 2); len(p) == 2 {
				Env.HostVod = strings.TrimSpace(p[1])
			}
			continue
		}
		if strings.HasPrefix(lineUpper, "LIVE_USER") {
			if p := strings.SplitN(line, "=", 2); len(p) == 2 {
				Env.MasterLiveUser = strings.TrimSpace(p[1])
			}
			continue
		}
		if strings.HasPrefix(lineUpper, "LIVE_PASS") {
			if p := strings.SplitN(line, "=", 2); len(p) == 2 {
				Env.MasterLivePass = strings.TrimSpace(p[1])
			}
			continue
		}
		if strings.HasPrefix(lineUpper, "VOD_USER") {
			if p := strings.SplitN(line, "=", 2); len(p) == 2 {
				Env.MasterVodUser = strings.TrimSpace(p[1])
			}
			continue
		}
		if strings.HasPrefix(lineUpper, "VOD_PASS") {
			if p := strings.SplitN(line, "=", 2); len(p) == 2 {
				Env.MasterVodPass = strings.TrimSpace(p[1])
			}
			continue
		}

		if strings.HasPrefix(lineUpper, "ADMIN_USER") {
			if p := strings.SplitN(line, "=", 2); len(p) == 2 {
				Env.AdminUser = strings.TrimSpace(p[1])
			}
			continue
		}
		if strings.HasPrefix(lineUpper, "ADMIN_PASS") {
			if p := strings.SplitN(line, "=", 2); len(p) == 2 {
				Env.AdminPass = strings.TrimSpace(p[1])
			}
			continue
		}
		if strings.HasPrefix(lineUpper, "ADMIN_DNS") || strings.HasPrefix(lineUpper, "DNS") {
			if p := strings.SplitN(line, "=", 2); len(p) == 2 {
				Env.AdminDNS = strings.TrimSpace(p[1])
			}
			continue
		}

		if strings.HasPrefix(lineUpper, "WEBPLAYER") {
			if p := strings.SplitN(line, "=", 2); len(p) == 2 {
				itens := strings.Split(p[1], ",")
				for _, item := range itens {
					item = strings.TrimSpace(item)
					if item != "" {
						if !strings.Contains(item, ":") && !isIPAddress(item) {
							ips, err := net.LookupHost(item)
							if err == nil {
								for _, ip := range ips {
									Env.WebPlayers = append(Env.WebPlayers, ip)
									fmt.Printf("   🌐 WebPlayer: %s → %s\n", item, ip)
								}
							} else {
								fmt.Printf("   ⚠️ WebPlayer DNS falhou: %s (%v)\n", item, err)
							}
							Env.WebPlayers = append(Env.WebPlayers, item)
						} else {
							Env.WebPlayers = append(Env.WebPlayers, item)
						}
					}
				}
			}
			continue
		}

		if lineUpper == "COMPLETO S/ ADULTOS" {
			secao = "FILTRO_ADULTOS"
			continue
		}
		if lineUpper == "OCULTA PARA TODOS" {
			secao = "OCULTA"
			continue
		}
		if lineUpper == "CANAIS" && !strings.Contains(line, "=") {
			secao = "ORDEM_CANAIS"
			ordemIdx = 0
			continue
		}
		if lineUpper == "FILMES" && !strings.Contains(line, "=") {
			secao = "ORDEM_FILMES"
			ordemIdx = 0
			continue
		}
		if lineUpper == "SERIES" && !strings.Contains(line, "=") {
			secao = "ORDEM_SERIES"
			ordemIdx = 0
			continue
		}

		switch secao {
		case "FILTRO_ADULTOS":
			partes := strings.SplitN(line, "=", 2)
			if len(partes) == 2 {
				tipo := strings.TrimSpace(strings.ToUpper(partes[0]))
				itens := strings.Split(partes[1], ",")
				for _, item := range itens {
					item = strings.TrimSpace(strings.Trim(strings.TrimSpace(item), `"`))
					if item != "" {
						Env.FiltroSemAdultos[tipo] = append(Env.FiltroSemAdultos[tipo], strings.ToUpper(item))
					}
				}
			}
		case "ORDEM_CANAIS":
			entry := parseCatLine(line, ordemIdx)
			Env.OrdemCanais[strings.ToUpper(entry.NomeOriginal)] = entry
			ordemIdx++
		case "ORDEM_FILMES":
			entry := parseCatLine(line, ordemIdx)
			Env.OrdemFilmes[strings.ToUpper(entry.NomeOriginal)] = entry
			ordemIdx++
		case "ORDEM_SERIES":
			entry := parseCatLine(line, ordemIdx)
			Env.OrdemSeries[strings.ToUpper(entry.NomeOriginal)] = entry
			ordemIdx++
		case "OCULTA":
			partes := strings.SplitN(line, "=", 2)
			if len(partes) == 2 {
				tipo := strings.TrimSpace(strings.ToUpper(partes[0]))
				itens := strings.Split(partes[1], ",")
				for _, item := range itens {
					item = strings.TrimSpace(strings.Trim(strings.TrimSpace(item), `"`))
					if item == "" {
						continue
					}
					switch tipo {
					case "CANAIS":
						Env.OcultaTodosCanais = append(Env.OcultaTodosCanais, strings.ToUpper(item))
					case "FILMES":
						Env.OcultaTodosFilmes = append(Env.OcultaTodosFilmes, strings.ToUpper(item))
					case "SERIES":
						Env.OcultaTodosSeries = append(Env.OcultaTodosSeries, strings.ToUpper(item))
					}
				}
			}
		}
	}

	if len(Env.SigmaPaineis) == 0 {
		Env.SigmaPaineis = []string{"GoMaIn123"}
	}
	if Env.AdminUser == "" {
		Env.AdminUser = "admin"
	}
	if Env.AdminPass == "" {
		Env.AdminPass = "admin123"
		fmt.Println("⚠️ ATENÇÃO: ADMIN_PASS não configurado! Usando senha padrão (admin123). TROQUE NO .env!")
	}

	if Env.HostLive != "" && Env.MasterLiveUser != "" {
		Env.M3ULive = fmt.Sprintf("http://%s/get.php?username=%s&password=%s&type=m3u_plus&output=mpegts", Env.HostLive, Env.MasterLiveUser, Env.MasterLivePass)
	}
	if Env.HostVod != "" && Env.MasterVodUser != "" {
		Env.M3UVod = fmt.Sprintf("http://%s/get.php?username=%s&password=%s&type=m3u_plus&output=mpegts", Env.HostVod, Env.MasterVodUser, Env.MasterVodPass)
	}

	if Env.HostLive == "" || Env.MasterLiveUser == "" {
		fmt.Println("⚠️ ATENÇÃO: Fonte LIVE não configurada no .env!")
	}

	fmt.Println("📋 .ENV Carregado:")
	fmt.Printf("   LIVE: %s (%s / %s)\n", Env.HostLive, Env.MasterLiveUser, Env.MasterLivePass)
	fmt.Printf("   VOD:  %s (%s / %s)\n", Env.HostVod, Env.MasterVodUser, Env.MasterVodPass)
	fmt.Printf("   FORMAT_CANAL = %s\n", Env.FormatoCanal)
	fmt.Printf("   SIGMA PAINÉIS = %v\n", Env.SigmaPaineis)
	fmt.Printf("   🔒 ADMIN: %s / %s\n", Env.AdminUser, strings.Repeat("*", len(Env.AdminPass)))
	fmt.Printf("   Filtro S/ Adultos: CANAIS=%d, FILMES=%d, SERIES=%d\n",
		len(Env.FiltroSemAdultos["CANAIS"]), len(Env.FiltroSemAdultos["FILMES"]), len(Env.FiltroSemAdultos["SERIES"]))
	fmt.Printf("   Ordem: CANAIS=%d, FILMES=%d, SERIES=%d\n",
		len(Env.OrdemCanais), len(Env.OrdemFilmes), len(Env.OrdemSeries))
	if len(Env.WebPlayers) > 0 {
		fmt.Printf("   🌐 WebPlayers confiáveis: %d entradas\n", len(Env.WebPlayers))
	}
}

func parseCatLine(line string, ordem int) CatEntry {
	if idx := strings.Index(line, "="); idx != -1 {
		return CatEntry{NomeOriginal: strings.TrimSpace(line[:idx]), NomeNovo: strings.TrimSpace(line[idx+1:]), Ordem: ordem}
	}
	return CatEntry{NomeOriginal: line, NomeNovo: line, Ordem: ordem}
}

func isWebPlayer(ip string) bool {
	for _, wp := range Env.WebPlayers {
		if ip == wp {
			return true
		}
	}
	return false
}

func isIPAddress(s string) bool {
	return net.ParseIP(s) != nil
}

// ⚡ DETECTOR DE APPS ANTIGOS (Injeta .ts na M3U pra evitar tela preta)
func isAppAntigo(ua string) bool {
	ua = strings.ToLower(ua)
	antigos := []string{"smart stb", "smart-stb", "ss iptv", "gse", "perfect player", "lazy iptv", "kodi", "vlc"}
	for _, a := range antigos {
		if strings.Contains(ua, a) {
			return true
		}
	}
	return false
}

func isOcultaParaTodos(catName, tipoConteudo string) bool {
	nameUpper := strings.ToUpper(catName)
	var lista []string
	switch tipoConteudo {
	case "live":
		lista = Env.OcultaTodosCanais
	case "movie", "vod":
		lista = Env.OcultaTodosFilmes
	case "series":
		lista = Env.OcultaTodosSeries
	}
	for _, oculta := range lista {
		if nameUpper == oculta || strings.Contains(nameUpper, oculta) || strings.Contains(oculta, nameUpper) {
			return true
		}
	}
	return false
}

func isFiltradaSemAdultos(catName, tipoConteudo string) bool {
	nameUpper := strings.ToUpper(catName)
	var chave string
	switch tipoConteudo {
	case "live":
		chave = "CANAIS"
	case "movie", "vod":
		chave = "FILMES"
	case "series":
		chave = "SERIES"
	}
	for _, filtrada := range Env.FiltroSemAdultos[chave] {
		if nameUpper == filtrada || strings.Contains(nameUpper, filtrada) || strings.Contains(filtrada, nameUpper) {
			return true
		}
	}
	return false
}

func getCatInfo(catName, tipoConteudo string) (string, int) {
	nameUpper := strings.ToUpper(catName)
	var mapa map[string]CatEntry
	switch tipoConteudo {
	case "live":
		mapa = Env.OrdemCanais
	case "movie", "vod":
		mapa = Env.OrdemFilmes
	case "series":
		mapa = Env.OrdemSeries
	}
	if entry, ok := mapa[nameUpper]; ok {
		return entry.NomeNovo, entry.Ordem
	}
	for chave, entry := range mapa {
		if strings.Contains(nameUpper, chave) || strings.Contains(chave, nameUpper) {
			return entry.NomeNovo, entry.Ordem
		}
	}
	return catName, 99999
}

// ============================================================
// ⚡ POOL DE BUFFERS
// ============================================================
var bufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 128*1024)
		return &buf
	},
}

// ============================================================
// ⚡ CACHE DE AUTENTICAÇÃO
// ============================================================
type AuthEntry struct {
	ExpDate  int64
	MaxCons  int
	Bouquet  string
	Valido   bool
	CachedAt time.Time
}

var (
	authCache     = make(map[string]AuthEntry)
	authCacheLock sync.RWMutex
	authCacheTTL  = 30 * time.Second
)

func validarUsuarioCache(user, pass string) (int64, int, string, bool) {
	if user == "" || pass == "" {
		return 0, 0, "", false
	}
	chave := user + ":" + pass
	authCacheLock.RLock()
	entry, ok := authCache[chave]
	authCacheLock.RUnlock()
	if ok && time.Since(entry.CachedAt) < authCacheTTL {
		return entry.ExpDate, entry.MaxCons, entry.Bouquet, entry.Valido
	}
	expDate, maxCons, bouquet, valido := validarUsuarioSQLite(user, pass)
	authCacheLock.Lock()
	authCache[chave] = AuthEntry{ExpDate: expDate, MaxCons: maxCons, Bouquet: bouquet, Valido: valido, CachedAt: time.Now()}
	authCacheLock.Unlock()
	return expDate, maxCons, bouquet, valido
}

func limparAuthCache() {
	for {
		time.Sleep(60 * time.Second)
		authCacheLock.Lock()
		agora := time.Now()
		for k, v := range authCache {
			if agora.Sub(v.CachedAt) > authCacheTTL*2 {
				delete(authCache, k)
			}
		}
		authCacheLock.Unlock()
	}
}

func invalidarAuthCache() {
	authCacheLock.Lock()
	authCache = make(map[string]AuthEntry)
	authCacheLock.Unlock()
}

// ============================================================
// 🌐 CONTROLE DE IP / TELAS
// ============================================================
type IPEntry struct {
	IP        string
	UltimoUso time.Time
}

var (
	ipTracker     = make(map[string][]IPEntry)
	ipTrackerLock sync.RWMutex
	ipTTL         = 30 * time.Minute
)

func pegarIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

func verificarIP(username, ip string, maxCons int) (bool, int, int) {
	ipTrackerLock.Lock()
	defer ipTrackerLock.Unlock()
	agora := time.Now()
	ips := ipTracker[username]
	var ativos []IPEntry
	for _, entry := range ips {
		if agora.Sub(entry.UltimoUso) < ipTTL {
			ativos = append(ativos, entry)
		}
	}
	encontrado := false
	for i, entry := range ativos {
		if entry.IP == ip {
			ativos[i].UltimoUso = agora
			encontrado = true
			break
		}
	}
	if !encontrado {
		if len(ativos) >= maxCons {
			ipTracker[username] = ativos
			fmt.Printf("🚫 BLOQUEADO: [%s] IP [%s] - %d/%d telas\n", username, ip, len(ativos), maxCons)
			return false, len(ativos), maxCons
		}
		ativos = append(ativos, IPEntry{IP: ip, UltimoUso: agora})
		fmt.Printf("📱 NOVO IP: [%s] IP [%s] (%d/%d telas)\n", username, ip, len(ativos), maxCons)
	}
	ipTracker[username] = ativos
	return true, len(ativos), maxCons
}

func limparIPsExpirados() {
	for {
		time.Sleep(30 * time.Minute)
		ipTrackerLock.Lock()
		agora := time.Now()
		for user, ips := range ipTracker {
			var ativos []IPEntry
			for _, entry := range ips {
				if agora.Sub(entry.UltimoUso) < ipTTL {
					ativos = append(ativos, entry)
				}
			}
			if len(ativos) == 0 {
				delete(ipTracker, user)
			} else {
				ipTracker[user] = ativos
			}
		}
		ipTrackerLock.Unlock()
	}
}

func getConexoesAtivas() (int, []map[string]string) {
	ipTrackerLock.RLock()
	defer ipTrackerLock.RUnlock()
	agora := time.Now()
	total := 0
	var lista []map[string]string
	for user, ips := range ipTracker {
		for _, entry := range ips {
			if agora.Sub(entry.UltimoUso) < ipTTL {
				total++
				lista = append(lista, map[string]string{"username": user, "ip": entry.IP, "expira": entry.UltimoUso.Add(ipTTL).Format("15:04:05")})
			}
		}
	}
	return total, lista
}

// ============================================================
// 🛡️ FUNÇÕES DE LOG E BLOQUEIO DE IPs
// ============================================================
func carregarIPsRegistrados() {
	ipsRegistradosLock.Lock()
	defer ipsRegistradosLock.Unlock()
	data, err := os.ReadFile(arquivoIPsClientes)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		partes := strings.SplitN(line, ",", 2)
		if len(partes) >= 1 {
			ip := strings.TrimSpace(partes[0])
			ip = strings.Trim(ip, `"`)
			if ip != "" {
				ipsRegistrados[ip] = true
			}
		}
	}
	fmt.Printf("📋 IPs já registrados carregados: %d\n", len(ipsRegistrados))
}

func carregarIPsBloqueados() {
	ipsBloqueadosLock.Lock()
	defer ipsBloqueadosLock.Unlock()
	info, err := os.Stat(arquivoIPsBloqueados)
	if err != nil {
		os.WriteFile(arquivoIPsBloqueados, []byte(""), 0644)
		ipsBloqueados = make(map[string]bool)
		return
	}
	if info.ModTime().Equal(ipsBloqueadosMod) { 
		return
	}
	ipsBloqueadosMod = info.ModTime()
	data, err := os.ReadFile(arquivoIPsBloqueados)
	if err != nil {
		return
	}
	novo := make(map[string]bool)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		ip := strings.TrimSpace(line)
		if ip != "" && !strings.HasPrefix(ip, "#") {
			novo[ip] = true
		}
	}
	ipsBloqueados = novo
	if len(ipsBloqueados) > 0 { 
		fmt.Printf("🛡️ IPs bloqueados carregados: %d\n", len(ipsBloqueados))
	}
}

func ipBloqueado(ip string) bool {
	ipsBloqueadosLock.RLock()
	defer ipsBloqueadosLock.RUnlock()
	return ipsBloqueados[ip]
}

func resolverOperadora(ip string) string {
	nomes, err := net.LookupAddr(ip)
	if err != nil || len(nomes) == 0 {
		return "Desconhecida"
	}
	nome := strings.TrimSuffix(nomes[0], ".")
	partes := strings.Split(nome, ".")
	if len(partes) >= 3 {
		return strings.Join(partes[len(partes)-3:], ".")
	}
	return nome
}

func registrarIPCliente(ip, username, userAgent string) {
	ipsRegistradosLock.RLock()
	jaExiste := ipsRegistrados[ip]
	ipsRegistradosLock.RUnlock()
	if jaExiste {
		return
	}
	ipsRegistradosLock.Lock()
	if ipsRegistrados[ip] {
		ipsRegistradosLock.Unlock()
		return
	}
	ipsRegistrados[ip] = true
	ipsRegistradosLock.Unlock()

	operadora := resolverOperadora(ip)
	userAgent = strings.ReplaceAll(userAgent, `"`, "'")

	linha := fmt.Sprintf(`"%s", "%s", "%s", "%s"`, ip, username, userAgent, operadora)
	f, err := os.OpenFile(arquivoIPsClientes, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.WriteString(linha + "\n")
		f.Close()
		fmt.Printf("📝 IP registrado: %s\n", linha)
	}
}

func monitorarIPsBloqueados() {
	for {
		time.Sleep(10 * time.Second)
		carregarIPsBloqueados()
	}
}

// ============================================================
// ⚡ GZIP HELPER
// ============================================================
var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
		return w
	},
}

// ============================================================
// 🌐 CORS
// ============================================================
func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

func escreverResposta(w http.ResponseWriter, r *http.Request, contentType string, dados []byte) {
	setCORS(w)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Connection", "keep-alive")
	aceitaGzip := r != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
	if len(dados) > 1024 && aceitaGzip {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w)
		gz.Write(dados)
		gz.Close()
		gzipWriterPool.Put(gz)
	} else {
		w.Write(dados)
	}
}

// ============================================================
// 🗄️ BANCO DE DADOS
// ============================================================
func iniciarBanco() {
	var err error
	db, err = sql.Open("sqlite3", "./usuarios.db?_journal_mode=WAL&_busy_timeout=5000&cache=shared")
	if err != nil {
		log.Fatal("❌ Erro SQLite:", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	query := `
	CREATE TABLE IF NOT EXISTS clientes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password TEXT NOT NULL,
		exp_date INTEGER NOT NULL,
		max_connections INTEGER DEFAULT 1,
		bouquet TEXT,
		created_at INTEGER,
		painel TEXT DEFAULT '',
		enabled INTEGER DEFAULT 1,
		admin_notes TEXT DEFAULT '',
		reseller_notes TEXT DEFAULT '',
		is_trial INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_username ON clientes(username);
	CREATE INDEX IF NOT EXISTS idx_painel ON clientes(painel);`
	_, err = db.Exec(query)
	if err != nil {
		log.Fatal("❌ Erro tabela:", err)
	}

	db.Exec("ALTER TABLE clientes ADD COLUMN painel TEXT DEFAULT ''")
	db.Exec("ALTER TABLE clientes ADD COLUMN enabled INTEGER DEFAULT 1")
	db.Exec("ALTER TABLE clientes ADD COLUMN admin_notes TEXT DEFAULT ''")
	db.Exec("ALTER TABLE clientes ADD COLUMN reseller_notes TEXT DEFAULT ''")
	db.Exec("ALTER TABLE clientes ADD COLUMN is_trial INTEGER DEFAULT 0")

	fmt.Println("✅ Banco NATIVO Ativado (com suporte multi-painel)!")
}

func mostrarBancoNoTerminal() {
	rows, err := db.Query("SELECT id, username, password, exp_date, max_connections, bouquet, painel, COALESCE(enabled, 1) FROM clientes")
	if err != nil {
		return
	}
	defer rows.Close()
	fmt.Println("\n====================================================================================================")
	fmt.Printf("%-5s | %-15s | %-12s | %-20s | %-5s | %-20s | %-12s | %-4s\n", "ID", "USUÁRIO", "SENHA", "VALIDADE", "TELAS", "PLANO", "PAINEL", "ON")
	fmt.Println("----------------------------------------------------------------------------------------------------")
	for rows.Next() {
		var id, maxCons, enabled int
		var user, pass, bouquet, painel string
		var expDate int64
		rows.Scan(&id, &user, &pass, &expDate, &maxCons, &bouquet, &painel, &enabled)
		dataExpStr := "Vitalício"
		if expDate > 0 {
			dataExpStr = time.Unix(expDate, 0).Format("02/01/2006 15:04:05")
		}
		if painel == "" {
			painel = "---"
		}
		statusStr := "✅"
		if enabled == 0 {
			statusStr = "🚫"
		} else if expDate > 0 && expDate < time.Now().Unix() {
			statusStr = "⏰"
		}
		fmt.Printf("%-5d | %-15s | %-12s | %-20s | %-5d | %-20s | %-12s | %-4s\n", id, user, pass, dataExpStr, maxCons, bouquet, painel, statusStr)
	}
	fmt.Println("====================================================================================================\n")
}

func mostrarEstatisticasPaineis() {
	fmt.Println("\n📊 ESTATÍSTICAS POR PAINEL SIGMA:")
	fmt.Println("============================================================")

	for _, painel := range Env.SigmaPaineis {
		var totalAtivos, totalTeste, totalExpirados, totalBloqueados int
		rows, err := db.Query("SELECT exp_date, COALESCE(enabled, 1), COALESCE(is_trial, 0) FROM clientes WHERE painel = ?", painel)
		if err != nil {
			continue
		}
		agora := time.Now().Unix()
		for rows.Next() {
			var expDate int64
			var enabled, isTrial int
			rows.Scan(&expDate, &enabled, &isTrial)

			if enabled == 0 {
				totalBloqueados++
			} else if expDate > 0 && expDate < agora {
				totalExpirados++
			} else if isTrial == 1 {
				totalTeste++
			} else {
				totalAtivos++
			}
		}
		rows.Close()
		total := totalAtivos + totalTeste + totalExpirados + totalBloqueados
		fmt.Printf("   📱 %-15s | %d ativos, %d teste, %d expirados, %d bloqueados | Total: %d\n",
			painel, totalAtivos, totalTeste, totalExpirados, totalBloqueados, total)
	}
	var semPainel int
	db.QueryRow("SELECT COUNT(*) FROM clientes WHERE painel = '' OR painel IS NULL").Scan(&semPainel)
	if semPainel > 0 {
		fmt.Printf("   ⚠️ %-15s | %d usuários (migração pendente)\n", "SEM PAINEL", semPainel)
	}
	fmt.Println("============================================================\n")
}

func parseExpDate(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return 0
	}
	if ts, err := strconv.ParseInt(raw, 10, 64); err == nil && ts > 1000000000 {
		return ts
	}
	loc := time.Local
	formatos := []string{
		"2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04",
		"2006-01-02 15:04", "2006-01-02", "02/01/2006 15:04:05", "02/01/2006",
	}
	for _, formato := range formatos {
		if t, err := time.ParseInLocation(formato, raw, loc); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// ============================================================
// 🤖 SIGMA HANDLER
// ============================================================
func detectarPainel(r *http.Request) string {
	path := r.URL.Path
	for _, painel := range Env.SigmaPaineis {
		if strings.Contains(path, painel) {
			return painel
		}
	}
	return ""
}

func sigmaHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	acao := r.FormValue("action")
	painel := detectarPainel(r)
	w.Header().Set("Content-Type", "application/json")

	switch acao {
	case "get_groups":
		w.Write([]byte(`[{"group_id":"1","group_name":"Administrators"},{"group_id":"2","group_name":"Resellers"}]`))

	case "get_packages":
		w.Write([]byte(`[{"id":"4","package_name":"COMPLETO C/ ADULTOS","is_addon":"0","is_trial":"1","is_official":"1","trial_credits":"0","official_credits":"0","trial_duration":"4","trial_duration_in":"hours","official_duration":"1","official_duration_in":"months","groups":"[2]","bouquets":"[5]","addon_packages":null,"is_line":"1","is_mag":"0","is_e2":"0","is_restreamer":"0","is_isplock":"0","output_formats":"[1,2,3]","max_connections":"5","force_server_id":"0","forced_country":null,"lock_device":"0","check_compatible":"1"},{"id":"5","package_name":"COMPLETO S/ ADULTOS","is_addon":"0","is_trial":"1","is_official":"1","trial_credits":"0","official_credits":"0","trial_duration":"4","trial_duration_in":"hours","official_duration":"1","official_duration_in":"months","groups":"[2]","bouquets":"[6]","addon_packages":null,"is_line":"1","is_mag":"0","is_e2":"0","is_restreamer":"0","is_isplock":"0","output_formats":"[1,2,3]","max_connections":"5","force_server_id":"0","forced_country":null,"lock_device":"0","check_compatible":"1"}]`))

case "create_line":
		user, pass := r.FormValue("username"), r.FormValue("password")
		maxCons, _ := strconv.Atoi(r.FormValue("max_connections"))
		
		if maxCons == 0 {
			maxCons = 1
		}

		// ⚡ TRAVA DE SEGURANÇA: MÁXIMO DE 2 CONEXÕES!
		if maxCons > 2 {
			w.Write([]byte(`{"status":"ERROR","message":"BLOQUEADO: O limite máximo permitido para criação é de 2 conexões!"}`))
			return // Isso faz o Go abortar a missão e não salvar no banco
		}

		var expUnix int64 = 0
		if rawExp := r.FormValue("exp_date"); rawExp != "" {
			expUnix = parseExpDate(rawExp)
		}
		enabled := 1
		if r.FormValue("enabled") == "0" {
			enabled = 0
		}
		bouquet := "COMPLETO C/ ADULTOS"
		if strings.Contains(r.FormValue("bouquets_selected"), "6") {
			bouquet = "COMPLETO S/ ADULTOS"
		}
		adminNotes := r.FormValue("admin_notes")
		resellerNotes := r.FormValue("reseller_notes")
		isTrial := 0
		if r.FormValue("is_trial") == "1" {
			isTrial = 1
		}
		agora := time.Now().Unix()

		var rowID int64
		res, err := db.Exec("INSERT INTO clientes (username, password, exp_date, max_connections, bouquet, created_at, painel, enabled, admin_notes, reseller_notes, is_trial) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			user, pass, expUnix, maxCons, bouquet, agora, painel, enabled, adminNotes, resellerNotes, isTrial)
		if err != nil {
			db.Exec("UPDATE clientes SET password=?, exp_date=?, max_connections=?, bouquet=?, enabled=?, admin_notes=?, reseller_notes=?, is_trial=? WHERE username=? AND painel=?",
				pass, expUnix, maxCons, bouquet, enabled, adminNotes, resellerNotes, isTrial, user, painel)
			db.QueryRow("SELECT id FROM clientes WHERE username=? AND painel=?", user, painel).Scan(&rowID)
		} else {
			rowID, _ = res.LastInsertId()
		}
		invalidarAuthCache()
		go mostrarBancoNoTerminal()

		status := "1"
		if enabled == 0 || (expUnix > 0 && expUnix < time.Now().Unix()) {
			status = "0"
		}
		w.Write([]byte(fmt.Sprintf(`{"status":"STATUS_SUCCESS","data":{"id":"%d","username":"%s","password":"%s","exp_date":"%d","max_connections":"%d","status":"%s","enabled":"%d"}}`,
			rowID, user, pass, expUnix, maxCons, status, enabled)))

	case "delete_line":
		idParaDeletar := r.FormValue("id")
		db.Exec("DELETE FROM clientes WHERE id=? AND painel=?", idParaDeletar, painel)
		invalidarAuthCache()
		go mostrarBancoNoTerminal()
		w.Write([]byte(`{"status":"STATUS_SUCCESS","message":"Linha deletada."}`))

	case "get_lines":
		search := r.FormValue("search[value]")
		var rows *sql.Rows
		var err error
		if search != "" {
			rows, err = db.Query("SELECT id, username, password, exp_date, max_connections, created_at, bouquet, COALESCE(enabled, 1), COALESCE(admin_notes, ''), COALESCE(is_trial, 0) FROM clientes WHERE painel = ? AND username LIKE ?", painel, "%"+search+"%")
		} else {
			rows, err = db.Query("SELECT id, username, password, exp_date, max_connections, created_at, bouquet, COALESCE(enabled, 1), COALESCE(admin_notes, ''), COALESCE(is_trial, 0) FROM clientes WHERE painel = ?", painel)
		}
		if err != nil {
			w.Write([]byte(`{"status":"STATUS_SUCCESS","data":[],"recordsTotal":"0","recordsFiltered":"0"}`))
			return
		}
		defer rows.Close()
		var buf bytes.Buffer
		buf.WriteString(`{"status":"STATUS_SUCCESS","data":[`)
		first := true
		total := 0
		for rows.Next() {
			var id, maxCons, enabled, isTrial int
			var user, pass, bouquet, adminNotes string
			var expDate, created int64
			rows.Scan(&id, &user, &pass, &expDate, &maxCons, &created, &bouquet, &enabled, &adminNotes, &isTrial)
			status := "1"
			if enabled == 0 || (expDate > 0 && expDate < time.Now().Unix()) {
				status = "0"
			}
			packageID := "4"
			if bouquet == "COMPLETO S/ ADULTOS" {
				packageID = "5"
			}
			if !first {
				buf.WriteByte(',')
			}
			fmt.Fprintf(&buf, `{"id":"%d","username":"%s","password":"%s","exp_date":"%d","max_connections":"%d","status":"%s","enabled":"%d","package_id":"%s","created_at":"%d","is_trial":"%d","active_cons":"0","admin_notes":"%s"}`,
				id, user, pass, expDate, maxCons, status, enabled, packageID, created, isTrial, adminNotes)
			first = false
			total++
		}
		fmt.Fprintf(&buf, `],"recordsTotal":"%d","recordsFiltered":"%d"}`, total, total)
		w.Write(buf.Bytes())

	case "get_users":
		resellerUser := r.FormValue("search[value]")
		if resellerUser == "" {
			resellerUser = painel
		}
		w.Write([]byte(fmt.Sprintf(`{"status":"STATUS_SUCCESS","data":[{"id":"1","username":"%s","password":"","email":"","member_group_id":"0","owner_id":"1","status":"1","credits":"100000","notes":"","date_registered":"%d","last_login":"%d"}],"recordsTotal":"1","recordsFiltered":"1"}`,
			resellerUser, time.Now().Unix()-86400*30, time.Now().Unix())))

	case "create_user":
		w.Write([]byte(fmt.Sprintf(`{"status":"STATUS_SUCCESS","data":{"id":"1","username":"%s","status":"1","credits":"100000"}}`, r.FormValue("username"))))

	case "edit_user":
		w.Write([]byte(fmt.Sprintf(`{"status":"STATUS_SUCCESS","data":{"id":"%s","username":"%s","status":"1","credits":"100000"}}`, r.FormValue("id"), r.FormValue("username"))))

	case "mysql_query":
		query := r.FormValue("query")
		limit, offset := 10000, 0
		queryUpper := strings.ToUpper(query)
		if idx := strings.Index(queryUpper, "LIMIT"); idx != -1 {
			limPart := strings.TrimSpace(query[idx+5:])
			parts := strings.Split(limPart, ",")
			if len(parts) == 2 {
				offset, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
				limit, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
			} else if len(parts) == 1 {
				limit, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
			}
		}
		rows, err := db.Query("SELECT id, username FROM clientes WHERE painel = ? LIMIT ? OFFSET ?", painel, limit, offset)
		if err != nil {
			w.Write([]byte(`{"status":"STATUS_SUCCESS","data":[]}`))
			return
		}
		defer rows.Close()
		var buf bytes.Buffer
		buf.WriteString(`{"status":"STATUS_SUCCESS","data":[`)
		first := true
		for rows.Next() {
			var id int
			var user string
			rows.Scan(&id, &user)
			if !first {
				buf.WriteByte(',')
			}
			fmt.Fprintf(&buf, `{"id":"%d","username":"%s"}`, id, user)
			first = false
		}
		buf.WriteString(`]}`)
		w.Write(buf.Bytes())

	case "live_connections":
		startParam, _ := strconv.Atoi(r.FormValue("start"))
		total, lista := getConexoesAtivas()
		var buf bytes.Buffer
		buf.WriteString(`{"status":"STATUS_SUCCESS","data":[`)
		if startParam == 0 {
			for i, c := range lista {
				if i > 0 {
					buf.WriteByte(',')
				}
				fmt.Fprintf(&buf, `{"username":"%s","ip":"%s","expira":"%s"}`, c["username"], c["ip"], c["expira"])
			}
		}
		fmt.Fprintf(&buf, `],"total":%d}`, total)
		w.Write(buf.Bytes())

	default:
		w.Write([]byte(`{"status":"STATUS_SUCCESS","message":"OK"}`))
	}
}

func validarUsuarioSQLite(user, pass string) (int64, int, string, bool) {
	if user == "" || pass == "" {
		return 0, 0, "", false
	}
	var dbPass, dbBouquet string
	var expDate int64
	var maxCons, enabled int
	err := db.QueryRow("SELECT password, exp_date, max_connections, bouquet, COALESCE(enabled, 1) FROM clientes WHERE username = ?", user).Scan(&dbPass, &expDate, &maxCons, &dbBouquet, &enabled)
	
	if err == nil && dbPass == pass {
		if enabled == 0 {
			return 0, 0, "", false
		}
		if expDate > 0 && expDate < time.Now().Unix() {
			return 0, 0, "", false
		}
		
		// ⚡ TOLERÂNCIA DE APLICATIVOS (A sua sacada de gênio)
		// O Sigma manda 1, mas a gente injeta +1 para os apps de nuvem não bloquearem a TV!
		maxCons = maxCons + 1
		
		return expDate, maxCons, dbBouquet, true
	}
	return 0, 0, "", false
}

// ============================================================
// 📦 CACHE DE DADOS
// ============================================================
var (
	ApiCacheCompleto = make(map[string][]byte)
	ApiCacheLivre    = make(map[string][]byte)
	OcultaCatIDs     sync.Map
	AdultoCatIDs     sync.Map
	M3UCompleto      []byte
	M3ULivre         []byte
)

func substituir(dados []byte, user, pass, host string) []byte {
	r := strings.NewReplacer("{USER}", user, "{PASS}", pass, "{HOST}", host)
	return []byte(r.Replace(string(dados)))
}

func limparURLs(rawStr, mUser, mPass string) string {
	rawStr = strings.ReplaceAll(rawStr, Env.HostLive+":80", "{HOST}")
	rawStr = strings.ReplaceAll(rawStr, Env.HostLive, "{HOST}")
	rawStr = strings.ReplaceAll(rawStr, `\/\/`+Env.HostLive, `\/\/{HOST}`)
	if Env.HostVod != Env.HostLive {
		rawStr = strings.ReplaceAll(rawStr, Env.HostVod+":80", "{HOST}")
		rawStr = strings.ReplaceAll(rawStr, Env.HostVod, "{HOST}")
		rawStr = strings.ReplaceAll(rawStr, `\/\/`+Env.HostVod, `\/\/{HOST}`)
	}
	rawStr = strings.ReplaceAll(rawStr, mUser, "{USER}")
	rawStr = strings.ReplaceAll(rawStr, mPass, "{PASS}")
	rawStr = trocarHostDesconhecido(rawStr, "http://")
	rawStr = trocarHostDesconhecido(rawStr, `http:\/\/`)
	rawStr = trocarHostDesconhecido(rawStr, "https://")
	rawStr = trocarHostDesconhecido(rawStr, `https:\/\/`)
	return rawStr
}

func trocarHostDesconhecido(rawStr, prefixo string) string {
	resultado := rawStr
	offset := 0
	for {
		idx := strings.Index(resultado[offset:], prefixo)
		if idx == -1 {
			break
		}
		pos := offset + idx + len(prefixo)
		if pos >= len(resultado) {
			break
		}
		if strings.HasPrefix(resultado[pos:], "{HOST}") {
			offset = pos + 6
			continue
		}
		hostFim := pos
		for hostFim < len(resultado) {
			c := resultado[hostFim]
			if c == '/' || c == '"' || c == '\'' || c == ' ' || c == '\n' {
				break
			}
			if c == '\\' && hostFim+1 < len(resultado) && resultado[hostFim+1] == '/' {
				break
			}
			hostFim++
		}
		if hostFim == pos {
			offset = hostFim + 1
			continue
		}
		host := resultado[pos:hostFim]
		restFim := hostFim
		for restFim < len(resultado) {
			c := resultado[restFim]
			if c == '"' || c == '\'' || c == ' ' || c == '\n' {
				break
			}
			restFim++
		}
		restoURL := resultado[hostFim:restFim]
		if strings.Contains(restoURL, "{USER}") {
			resultado = resultado[:pos] + "{HOST}" + resultado[pos+len(host):]
			offset = pos + 6
		} else {
			offset = hostFim
		}
	}
	return resultado
}

func getHostPara(mUser string) string {
	if mUser == Env.MasterLiveUser {
		return Env.HostLive
	}
	return Env.HostVod
}

func linkExpoeCredenciais(link string) bool {
	return (Env.MasterLiveUser != "" && strings.Contains(link, Env.MasterLiveUser)) ||
		(Env.MasterLivePass != "" && strings.Contains(link, Env.MasterLivePass)) ||
		(Env.MasterVodUser != "" && strings.Contains(link, Env.MasterVodUser)) ||
		(Env.MasterVodPass != "" && strings.Contains(link, Env.MasterVodPass))
}

func copiarMap(original map[string]interface{}) map[string]interface{} {
	copia := make(map[string]interface{}, len(original))
	for k, v := range original {
		copia[k] = v
	}
	return copia
}

func detectarTipoConteudo(action string) string {
	if strings.Contains(action, "live") {
		return "live"
	}
	if strings.Contains(action, "series") {
		return "series"
	}
	return "movie"
}

// ============================================================
// 🔄 ATUALIZAÇÃO DA MATRIZ
// ============================================================
func atualizarTudo() {
	fmt.Println("🔄 Atualizando Matriz...")
	OcultaCatIDs     = sync.Map{}
	AdultoCatIDs     = sync.Map{}

	catLiveC, catLiveL := fetchAndFilterCategories("get_live_categories", Env.MasterLiveUser, Env.MasterLivePass, "live")
	catVodC, catVodL := fetchAndFilterCategories("get_vod_categories", Env.MasterVodUser, Env.MasterVodPass, "movie")
	catSerC, catSerL := fetchAndFilterCategories("get_series_categories", Env.MasterVodUser, Env.MasterVodPass, "series")
	strLiveC, strLiveL := fetchAndFilterStreams("get_live_streams", Env.MasterLiveUser, Env.MasterLivePass, "live")
	strVodC, strVodL := fetchAndFilterStreams("get_vod_streams", Env.MasterVodUser, Env.MasterVodPass, "movie")
	strSerC, strSerL := fetchAndFilterStreams("get_series", Env.MasterVodUser, Env.MasterVodPass, "series")

	CacheLock.Lock()
	ApiCacheCompleto["get_live_categories"] = catLiveC
	ApiCacheLivre["get_live_categories"] = catLiveL
	ApiCacheCompleto["get_live_streams"] = strLiveC
	ApiCacheLivre["get_live_streams"] = strLiveL
	ApiCacheCompleto["get_vod_categories"] = catVodC
	ApiCacheLivre["get_vod_categories"] = catVodL
	ApiCacheCompleto["get_vod_streams"] = strVodC
	ApiCacheLivre["get_vod_streams"] = strVodL
	ApiCacheCompleto["get_series_categories"] = catSerC
	ApiCacheLivre["get_series_categories"] = catSerL
	ApiCacheCompleto["get_series"] = strSerC
	ApiCacheLivre["get_series"] = strSerL
	CacheLock.Unlock()

	liveC, liveL := baixarEFiltrarM3U(Env.M3ULive, "live", Env.MasterLiveUser, Env.MasterLivePass)
	vodC, vodL := baixarEFiltrarM3U(Env.M3UVod, "movie", Env.MasterVodUser, Env.MasterVodPass)

	CacheLock.Lock()
	M3UCompleto = []byte("#EXTM3U\n" + liveC + vodC)
	M3ULivre = []byte("#EXTM3U\n" + liveL + vodL)
	CacheLock.Unlock()
	fmt.Println("✅ Matriz atualizada com sucesso!")
}

func marshalJSONNoEscape(v interface{}) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimSpace(buf.Bytes())
}

func fetchAndFilterCategories(action, mUser, mPass, tipoConteudo string) ([]byte, []byte) {
	apiURL := fmt.Sprintf("http://%s/player_api.php?username=%s&password=%s&action=%s", getHostPara(mUser), mUser, mPass, action)
	// ⚡ Repasse do User-Agent na API para não ser bloqueado pela matriz
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("User-Agent", "IPTV Smarters Pro") 
	resp, err := httpClient.Do(req)
	if err != nil {
		return []byte("[]"), []byte("[]")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	rawStr := limparURLs(string(raw), mUser, mPass)

	var dados []map[string]interface{}
	if err := json.Unmarshal([]byte(rawStr), &dados); err != nil {
		b := []byte(rawStr)
		return b, b
	}

	var completos, livres []map[string]interface{}
	for _, item := range dados {
		catName, _ := item["category_name"].(string)
		catID := fmt.Sprintf("%v", item["category_id"])
		if isOcultaParaTodos(catName, tipoConteudo) {
			OcultaCatIDs.Store(catID, true)
			continue
		}
		novoNome, ordem := getCatInfo(catName, tipoConteudo)
		itemCopy := copiarMap(item)
		if novoNome != catName {
			itemCopy["category_name"] = novoNome
		}
		itemCopy["_ordem"] = float64(ordem)
		if isFiltradaSemAdultos(catName, tipoConteudo) {
			AdultoCatIDs.Store(catID, true)
			completos = append(completos, itemCopy)
			continue
		}
		completos = append(completos, itemCopy)
		livres = append(livres, itemCopy)
	}

	sortCats := func(cats []map[string]interface{}) {
		sort.SliceStable(cats, func(i, j int) bool {
			oi, _ := cats[i]["_ordem"].(float64)
			oj, _ := cats[j]["_ordem"].(float64)
			return oi < oj
		})
		for _, c := range cats {
			delete(c, "_ordem")
		}
	}
	sortCats(completos)
	sortCats(livres)

	bC := marshalJSONNoEscape(completos)
	bL := marshalJSONNoEscape(livres)
	if completos == nil {
		bC = []byte("[]")
	}
	if livres == nil {
		bL = []byte("[]")
	}
	return bC, bL
}

func fetchAndFilterStreams(action, mUser, mPass, tipoConteudo string) ([]byte, []byte) {
	apiURL := fmt.Sprintf("http://%s/player_api.php?username=%s&password=%s&action=%s", getHostPara(mUser), mUser, mPass, action)
	// ⚡ Repasse do User-Agent na API para não ser bloqueado pela matriz
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("User-Agent", "IPTV Smarters Pro") 
	resp, err := httpClient.Do(req)
	if err != nil {
		return []byte("[]"), []byte("[]")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	rawStr := limparURLs(string(raw), mUser, mPass)

	var items []json.RawMessage
	if err := json.Unmarshal([]byte(rawStr), &items); err != nil {
		b := []byte(rawStr)
		return b, b
	}

	var bufC, bufL bytes.Buffer
	bufC.WriteByte('[')
	bufL.WriteByte('[')
	firstC, firstL := true, true

	for _, rawItem := range items {
		var peek struct {
			CatID interface{} `json:"category_id"`
		}
		json.Unmarshal(rawItem, &peek)
		catID := fmt.Sprintf("%v", peek.CatID)

		if _, oculta := OcultaCatIDs.Load(catID); oculta {
			continue
		}

		if !firstC {
			bufC.WriteByte(',')
		}
		bufC.Write(rawItem)
		firstC = false

		if _, adulto := AdultoCatIDs.Load(catID); adulto {
			continue
		}
		if !firstL {
			bufL.WriteByte(',')
		}
		bufL.Write(rawItem)
		firstL = false
	}

	bufC.WriteByte(']')
	bufL.WriteByte(']')
	return bufC.Bytes(), bufL.Bytes()
}

// ============================================================
// 📺 M3U
// ============================================================
type M3UEntry struct {
	InfoLine string
	URLLine  string
	Group    string
	Ordem    int
}

func extrairGroupTitle(line string) string {
	idx := strings.Index(line, `group-title="`)
	if idx == -1 {
		return ""
	}
	inicio := idx + len(`group-title="`)
	fim := strings.Index(line[inicio:], `"`)
	if fim == -1 {
		return ""
	}
	return line[inicio : inicio+fim]
}

func renomearGroupTitle(line, velho, novo string) string {
	return strings.Replace(line, `group-title="`+velho+`"`, `group-title="`+novo+`"`, 1)
}

func formatarURLM3U(line string, pastaBase string) string {
	partes := strings.Split(line, "/")
	if len(partes) >= 4 {
		arquivo := partes[len(partes)-1]
		id, ext := arquivo, ""
		if idx := strings.LastIndex(arquivo, "."); idx != -1 {
			id, ext = arquivo[:idx], strings.ToLower(arquivo[idx:])
		}
		pasta := pastaBase
		if strings.Contains(line, "/series/") {
			pasta = "series"
		}
		
		// ⚡ AQUI MUDA: Colocamos o curinga {EXT_LIVE} nos canais ao vivo (Links Limpos)
		if pastaBase == "live" {
			return fmt.Sprintf("http://{HOST}/%s/{USER}/{PASS}/%s{EXT_LIVE}", pasta, id)
		} else if ext != ".mp4" && ext != ".mkv" && ext != ".avi" {
			ext = ".m3u8"
		}
		return fmt.Sprintf("http://{HOST}/%s/{USER}/{PASS}/%s%s", pasta, id, ext)
	}
	return line
}

func baixarEFiltrarM3U(m3uURL, pastaBase, mUser, mPass string) (string, string) {
	req, _ := http.NewRequest("GET", m3uURL, nil)
	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return "", ""
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024*50)

	var entradas []M3UEntry
	var infoLine string
	esperandoURL := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.Contains(line, "#EXTM3U") {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF") {
			infoLine = line
			esperandoURL = true
			continue
		}
		if esperandoURL && strings.HasPrefix(line, "http") {
			group := extrairGroupTitle(infoLine)
			if isOcultaParaTodos(group, pastaBase) {
				esperandoURL = false
				continue
			}
			novoNome, ordem := getCatInfo(group, pastaBase)
			finalInfo := infoLine
			if novoNome != group && group != "" {
				finalInfo = renomearGroupTitle(infoLine, group, novoNome)
			}
			entradas = append(entradas, M3UEntry{InfoLine: finalInfo, URLLine: formatarURLM3U(line, pastaBase), Group: group, Ordem: ordem})
			esperandoURL = false
			continue
		}
	}

	sort.SliceStable(entradas, func(i, j int) bool { return entradas[i].Ordem < entradas[j].Ordem })

	var sbC, sbL strings.Builder
	sbC.Grow(1024 * 1024)
	sbL.Grow(1024 * 1024)
	for _, e := range entradas {
		sbC.WriteString(e.InfoLine + "\n" + e.URLLine + "\n")
		if !isFiltradaSemAdultos(e.Group, pastaBase) {
			sbL.WriteString(e.InfoLine + "\n" + e.URLLine + "\n")
		}
	}
	return sbC.String(), sbL.String()
}

// ============================================================
// 🌐 XTREAM API HANDLER
// ============================================================
func xtreamAPIHandler(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}

	u := r.URL.Query().Get("username")
	p := r.URL.Query().Get("password")
	action := r.URL.Query().Get("action")

	expDate, maxCons, bouquet, valido := validarUsuarioCache(u, p)
	if !valido {
		if action == "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"user_info":{"auth":0}}`))
		} else {
			http.Error(w, "NEGADO", 401)
		}
		return
	}

	ip := pegarIP(r)

	if ipBloqueado(ip) {
		http.Error(w, "ACESSO NEGADO", 403)
		return
	}

	ativas := 0
	if !isWebPlayer(ip) {
		permitido, a, _ := verificarIP(u, ip, maxCons)
		ativas = a
		go registrarIPCliente(ip, u, r.Header.Get("User-Agent"))
		if !permitido {
			if action == "" {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(fmt.Sprintf(`{"user_info":{"auth":1,"status":"Active","username":"%s","max_connections":"%d","active_cons":"%d","message":"Limite de telas atingido."}}`, u, maxCons, ativas)))
			} else {
				http.Error(w, "LIMITE DE TELAS", 403)
			}
			return
		}
	}

	if action == "" {
		agora := time.Now()
		expStr := fmt.Sprintf(`"%d"`, expDate)
		if expDate == 0 {
			expStr = `"null"`
		}
		var buf bytes.Buffer
		buf.Grow(512)
		fmt.Fprintf(&buf, `{"user_info":{"username":"%s","password":"%s","message":"Welcome to XUI.one","auth":1,"status":"Active","exp_date":%s,"is_trial":"0","active_cons":"%d","created_at":"%d","max_connections":"%d","allowed_output_formats":["m3u8","ts","rtmp"]},"server_info":{"xui":true,"version":"1.5.13","revision":null,"url":"%s","port":"80","https_port":"443","server_protocol":"http","rtmp_port":"8880","timezone":"America/Sao_Paulo","timestamp_now":%d,"time_now":"%s"}}`,
			u, p, expStr, ativas, agora.Unix()-86400, maxCons, r.Host, agora.Unix(), agora.Format("2006-01-02 15:04:05"))
		escreverResposta(w, r, "application/json", buf.Bytes())
		return
	}

	isHeavy := action == "get_live_categories" || action == "get_vod_categories" || action == "get_series_categories" ||
		action == "get_live_streams" || action == "get_vod_streams" || action == "get_series"

	if isHeavy {
		CacheLock.RLock()
		var respostaCache []byte
		if bouquet == "COMPLETO S/ ADULTOS" {
			respostaCache = ApiCacheLivre[action]
		} else {
			respostaCache = ApiCacheCompleto[action]
		}
		CacheLock.RUnlock()

		if len(respostaCache) == 0 {
			tipoConteudo := detectarTipoConteudo(action)
			var mUser, mPass string
			if strings.Contains(action, "live") {
				mUser, mPass = Env.MasterLiveUser, Env.MasterLivePass
			} else {
				mUser, mPass = Env.MasterVodUser, Env.MasterVodPass
			}
			var cC, cL []byte
			if strings.Contains(action, "categories") {
				cC, cL = fetchAndFilterCategories(action, mUser, mPass, tipoConteudo)
			} else {
				cC, cL = fetchAndFilterStreams(action, mUser, mPass, tipoConteudo)
			}
			CacheLock.Lock()
			ApiCacheCompleto[action] = cC
			ApiCacheLivre[action] = cL
			CacheLock.Unlock()
			if bouquet == "COMPLETO S/ ADULTOS" {
				respostaCache = cL
			} else {
				respostaCache = cC
			}
		}

		// ⚡ MÁGICA ANTI-BLOQUEIO NA API XTREAM: Substitui na lista JSON também!
		resultado := substituir(respostaCache, u, p, r.Host)
		
		ua := r.Header.Get("User-Agent")
		extensao := ""
		if isAppAntigo(ua) {
			extensao = Env.FormatoCanal
		}
		resultado = bytes.ReplaceAll(resultado, []byte("{EXT_LIVE}"), []byte(extensao))

		escreverResposta(w, r, "application/json", resultado)
		return
	}

	var mUser, mPass string
	if action == "get_short_epg" {
		mUser, mPass = Env.MasterLiveUser, Env.MasterLivePass
	} else {
		mUser, mPass = Env.MasterVodUser, Env.MasterVodPass
	}
	urlFinal := fmt.Sprintf("http://%s/player_api.php?username=%s&password=%s&action=%s", getHostPara(mUser), mUser, mPass, action)
	for k, v := range r.URL.Query() {
		if k != "username" && k != "password" && k != "action" {
			urlFinal += fmt.Sprintf("&%s=%s", k, v[0])
		}
	}
	reqUp, _ := http.NewRequest("GET", urlFinal, nil)
	// ⚡ Repasse do User-Agent para EPG e Info de VOD/Séries
	reqUp.Header.Set("User-Agent", r.Header.Get("User-Agent"))
	
	resp, err := httpClient.Do(reqUp)
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		limpo := limparURLs(string(body), mUser, mPass)
		resultado := strings.ReplaceAll(limpo, "{USER}", u)
		resultado = strings.ReplaceAll(resultado, "{PASS}", p)
		resultado = strings.ReplaceAll(resultado, "{HOST}", r.Host)
		escreverResposta(w, r, "application/json", []byte(resultado))
	}
}


func gerarListaM3U(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}

	u := r.URL.Query().Get("username")
	p := r.URL.Query().Get("password")
	_, maxCons, bouquet, valido := validarUsuarioCache(u, p)
	if !valido {
		http.Error(w, "ACESSO NEGADO", 401)
		return
	}
	
	ip := pegarIP(r)
	if ipBloqueado(ip) {
		http.Error(w, "ACESSO NEGADO", 403)
		return
	}
	
	if !isWebPlayer(ip) {
		permitido, _, _ := verificarIP(u, ip, maxCons)
		go registrarIPCliente(ip, u, r.Header.Get("User-Agent"))
		if !permitido {
			http.Error(w, "LIMITE DE TELAS ATINGIDO", 403)
			return
		}
	}

	// 1. Pega a matriz bruta da Memória RAM (Instantâneo)
	CacheLock.RLock()
	var lista []byte
	if bouquet == "COMPLETO S/ ADULTOS" {
		lista = M3ULivre
	} else {
		lista = M3UCompleto
	}
	CacheLock.RUnlock()

    // 2. Verifica output e app antigo (Estratégia Anti-Bloqueio)
	outputParam := strings.ToLower(r.URL.Query().Get("output"))
	ua := r.Header.Get("User-Agent")
	extensao := ""

	// ⚡ ESTRATÉGIA ANTI-BLOQUEIO
	if outputParam == "hls" {
		// Se pediu HLS explicitamente, mantém o .m3u8
		extensao = ".m3u8"
	} else if isAppAntigo(ua) {
		// Se for SS IPTV, Smart STB, Kodi, VLC, etc., injeta o .ts para não dar tela preta
		extensao = Env.FormatoCanal
		fmt.Printf("📺 App Antigo detectado [%s] - Injetando %s na M3U\n", ua, extensao)
	} else {
		// 🛡️ O SEGREDO: Para o resto (apps modernos), fica VAZIO. Removemos o .ts!
		extensao = ""
	}
	
	// 3. A MÁGICA DA ACELERAÇÃO: O Replacer Mestre
	replacer := strings.NewReplacer(
		"{USER}", u,
		"{PASS}", p,
		"{HOST}", r.Host,
		"{EXT_LIVE}", extensao,
	)

	// Prepara os cabeçalhos de resposta
	w.Header().Set("Content-Type", "application/x-mpegurl")
	w.Header().Set("Connection", "keep-alive")
	
	// ⚡ A MÁGICA PARA FORÇAR O DOWNLOAD NO NAVEGADOR
	nomeArquivo := "lista_king.m3u"
	if outputParam == "hls" {
		nomeArquivo = "lista_king_hls.m3u"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, nomeArquivo))

	// 4. O TUBO DIRETO (Compressão GZIP)
	aceitaGzip := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
	
	if aceitaGzip {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w)
		
		// O Replacer escreve o texto com os dados do cliente DIRETO no compressor
		replacer.WriteString(gz, string(lista))
		
		gz.Close()
		gzipWriterPool.Put(gz)
	} else {
		// Se o app não suportar GZIP, envia direto
		replacer.WriteString(w, string(lista))
	}
}

// ============================================================
// ▶️ PLAY HANDLER
// ============================================================
func playHandler(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}

	caminho := strings.TrimPrefix(r.URL.Path, "/")
	if caminho == "" || caminho == "favicon.ico" {
		w.WriteHeader(http.StatusOK)
		return
	}
	partes := strings.Split(caminho, "/")
	tipo := ""
	if len(partes) >= 4 && (partes[0] == "live" || partes[0] == "movie" || partes[0] == "series") {
		tipo, partes = partes[0], partes[1:]
	}
	if len(partes) < 3 {
		http.Error(w, "Formato invalido", 400)
		return
	}

	user, pass, arquivo := partes[0], partes[1], partes[2]
	if len(partes) > 3 {
		arquivo = strings.Join(partes[2:], "/")
	}

	_, maxCons, _, valido := validarUsuarioCache(user, pass)
	if !valido {
		http.Error(w, "NEGADO", 401)
		return
	}
	ip := pegarIP(r)
	if ipBloqueado(ip) {
		http.Error(w, "ACESSO NEGADO", 403)
		return
	}
	if !isWebPlayer(ip) {
		permitido, _, _ := verificarIP(user, ip, maxCons)
		go registrarIPCliente(ip, user, r.Header.Get("User-Agent"))
		if !permitido {
			http.Error(w, "LIMITE DE TELAS", 403)
			return
		}
	}

	ultimaParte := partes[len(partes)-1]
	idStr := ultimaParte
	extPedida := ""
	if idx := strings.LastIndex(ultimaParte, "."); idx != -1 {
		idStr = ultimaParte[:idx]
		extPedida = strings.ToLower(ultimaParte[idx:])
	}

	pasta := tipo
	if pasta == "" {
		if extPedida == ".mp4" || extPedida == ".mkv" || extPedida == ".avi" {
			pasta = "movie"
		} else if extPedida == ".m3u8" {
			pasta = "live"
		} else {
			pasta = "live"
		}
	}

	// ⚡ FORMATO INTELIGENTE: Pare de tentar adivinhar! Seja um espelho.
	if pasta == "live" {
		if extPedida != "" {
			// Cenário 1: O app pediu com extensão explícita (ex: .m3u8 ou .ts)
			arquivo = idStr + extPedida
		} else {
			// Cenário 2: O app pediu SEM extensão (O novo padrão XUI anti-bloqueio)
			arquivo = idStr
		}
	} else {
		// Movie/Series — respeita a extensão pedida
		if extPedida != "" {
			arquivo = idStr + extPedida
		} else {
			arquivo = idStr + ".mp4"
		}
		if len(partes) > 3 {
			subPartes := partes[2:]
			ultimoIdx := len(subPartes) - 1
			nomeFinal := subPartes[ultimoIdx]
			if idx := strings.LastIndex(nomeFinal, "."); idx == -1 {
				subPartes[ultimoIdx] = nomeFinal + ".mp4"
			}
			arquivo = strings.Join(subPartes, "/")
		}
	}

	var masterUser, masterPass string
	if pasta == "live" {
		masterUser, masterPass = Env.MasterLiveUser, Env.MasterLivePass
	} else {
		masterUser, masterPass = Env.MasterVodUser, Env.MasterVodPass
	}

	urlMaster := fmt.Sprintf("http://%s/%s/%s/%s/%s", getHostPara(masterUser), pasta, masterUser, masterPass, arquivo)
	reqOut, _ := http.NewRequest("GET", urlMaster, nil)
	
	// ⚡ REPASSE DE HEADERS COMPLETOS (User-Agent e Range de Filmes VOD)
	reqOut.Header.Set("User-Agent", r.Header.Get("User-Agent"))
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		reqOut.Header.Set("Range", rangeHeader)
	}

	resp, err := clientAcelerado.Do(reqOut)
	if err != nil {
		http.Error(w, "Erro ao contatar fonte", 500)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 301 || resp.StatusCode == 302 || resp.StatusCode == 303 {
		linkFinal := resp.Header.Get("Location")
		if linkFinal != "" {
			if linkExpoeCredenciais(linkFinal) {
				proxyStream(w, r, urlMaster)
				return
			}
			http.Redirect(w, r, linkFinal, http.StatusFound)
			return
		}
	}
	linkFinal := resp.Request.URL.String()
	if linkExpoeCredenciais(linkFinal) {
		proxyStream(w, r, urlMaster)
		return
	}
	http.Redirect(w, r, linkFinal, http.StatusFound)
}

func proxyStream(w http.ResponseWriter, r *http.Request, targetURL string) {
	setCORS(w)
	reqP, _ := http.NewRequest(r.Method, targetURL, nil)
	
	// ⚡ REPASSE DE HEADERS: Essencial para VOD Range e TV Boxes chatas
	for k, vv := range r.Header {
		for _, v := range vv {
			reqP.Header.Add(k, v)
		}
	}
	
	resp, err := streamClient.Do(reqP) 
	if err != nil {
		http.Error(w, "Offline", 502)
		return
	}
	defer resp.Body.Close()
	
	// ⚡ Repassa os Headers da resposta do XUI para a TV (ex: Content-Range)
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	
	if strings.HasSuffix(targetURL, ".ts") {
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Del("Content-Length")
	}
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	
	// ⚡ Se a matriz retornar 206 Partial Content (Filmes), a gente repassa o 206!
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)
	bufPtr := bufPool.Get().(*[]byte)
	defer bufPool.Put(bufPtr)
	buf := *bufPtr
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}
}

// ============================================================
// 📊 ENDPOINT DE STATS (acessa via navegador)
// ============================================================
func statsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	agora := time.Now().Unix()
	var resultado []map[string]interface{}
	for _, painel := range Env.SigmaPaineis {
		var totalAtivos, totalTeste, totalExpirados, totalBloqueados int
		rows, err := db.Query("SELECT exp_date, COALESCE(enabled, 1), COALESCE(is_trial, 0) FROM clientes WHERE painel = ?", painel)
		if err != nil {
			continue
		}
		for rows.Next() {
			var expDate int64
			var enabled, isTrial int
			rows.Scan(&expDate, &enabled, &isTrial)
			if enabled == 0 {
				totalBloqueados++
			} else if isTrial == 1 {
				totalTeste++
			} else if expDate > 0 && expDate < agora {
				totalExpirados++
			} else {
				totalAtivos++
			}
		}
		rows.Close()
		resultado = append(resultado, map[string]interface{}{
			"painel": painel, "ativos": totalAtivos, "teste": totalTeste,
			"expirados": totalExpirados, "bloqueados": totalBloqueados,
			"total": totalAtivos + totalTeste + totalExpirados + totalBloqueados,
		})
	}
	json.NewEncoder(w).Encode(resultado)
}

func verificarAdmin(w http.ResponseWriter, r *http.Request) bool {
	user, pass, ok := r.BasicAuth()
	if ok && user == Env.AdminUser && pass == Env.AdminPass {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="GO IPTV Admin"`)
	http.Error(w, "Acesso negado", 401)
	return false
}

func apiUsuariosHandler(w http.ResponseWriter, r *http.Request) {
	if !verificarAdmin(w, r) { return }
	w.Header().Set("Content-Type", "application/json")
	search := r.URL.Query().Get("search")
	painelFiltro := r.URL.Query().Get("painel")
	statusFiltro := r.URL.Query().Get("status")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 { page = 1 }
	perPage := 100
	agora := time.Now().Unix()

	query := "SELECT id, username, password, exp_date, max_connections, bouquet, painel, COALESCE(enabled, 1), COALESCE(admin_notes, ''), COALESCE(created_at, 0), COALESCE(is_trial, 0) FROM clientes WHERE 1=1"
	countQuery := "SELECT COUNT(*) FROM clientes WHERE 1=1"
	args := []interface{}{}
	if painelFiltro != "" && painelFiltro != "todos" {
		query += " AND painel = ?"
		countQuery += " AND painel = ?"
		args = append(args, painelFiltro)
	}
	if search != "" {
		query += " AND username LIKE ?"
		countQuery += " AND username LIKE ?"
		args = append(args, "%"+search+"%")
	}

	var total int
	db.QueryRow(countQuery, args...).Scan(&total)

	query += " ORDER BY id DESC LIMIT ? OFFSET ?"
	queryArgs := append(args, perPage, (page-1)*perPage)
	rows, err := db.Query(query, queryArgs...)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"data": []interface{}{}, "total": 0, "page": page, "pages": 0})
		return
	}
	defer rows.Close()
	var lista []map[string]interface{}
	host := Env.AdminDNS
	if host == "" { host = Env.HostLive }
	for rows.Next() {
		var id, maxCons, enabled, isTrial int
		var user, pass, bouquet, painel, notes string
		var expDate, created int64
		rows.Scan(&id, &user, &pass, &expDate, &maxCons, &bouquet, &painel, &enabled, &notes, &created, &isTrial)
		status := "ativo"
		if enabled == 0 {
			status = "bloqueado"
		} else if expDate > 0 && expDate < agora {
			status = "expirado"
		} else if isTrial == 1 {
			status = "teste"
		}
		if statusFiltro != "" && statusFiltro != "todos" && status != statusFiltro { continue }
		validade := "Vitalicio"
		if expDate > 0 { validade = time.Unix(expDate, 0).Format("02/01/2006 15:04") }
		criado := ""
		if created > 0 { criado = time.Unix(created, 0).Format("02/01/2006 15:04") }
		linkM3U := fmt.Sprintf("http://%s/get.php?username=%s&password=%s&type=m3u_plus&output=mpegts", host, user, pass)
		linkHLS := fmt.Sprintf("http://%s/get.php?username=%s&password=%s&type=m3u_plus&output=hls", host, user, pass)
		lista = append(lista, map[string]interface{}{
			"id": id, "username": user, "password": pass, "exp_date": expDate,
			"validade": validade, "max_connections": maxCons, "bouquet": bouquet,
			"painel": painel, "enabled": enabled, "status": status, "notes": notes,
			"created_at": criado, "is_trial": isTrial, "link_m3u": linkM3U, "link_hls": linkHLS,
		})
	}
	if lista == nil { lista = []map[string]interface{}{} }
	pages := (total + perPage - 1) / perPage
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": lista, "total": total, "page": page, "pages": pages, "per_page": perPage,
	})
}

func apiToggleHandler(w http.ResponseWriter, r *http.Request) {
	if !verificarAdmin(w, r) { return }
	w.Header().Set("Content-Type", "application/json")
	id := r.URL.Query().Get("id")
	acao := r.URL.Query().Get("acao")
	novoEnabled := 1
	if acao == "bloquear" { novoEnabled = 0 }
	db.Exec("UPDATE clientes SET enabled = ? WHERE id = ?", novoEnabled, id)
	invalidarAuthCache()
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

func apiDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if !verificarAdmin(w, r) { return }
	w.Header().Set("Content-Type", "application/json")
	id := r.URL.Query().Get("id")
	db.Exec("DELETE FROM clientes WHERE id = ?", id)
	invalidarAuthCache()
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

func apiDeleteExpiredHandler(w http.ResponseWriter, r *http.Request) {
	if !verificarAdmin(w, r) { return }
	w.Header().Set("Content-Type", "application/json")
	painelFiltro := r.URL.Query().Get("painel")
	agora := time.Now().Unix()
	var res sql.Result
	if painelFiltro != "" && painelFiltro != "todos" {
		res, _ = db.Exec("DELETE FROM clientes WHERE exp_date > 0 AND exp_date < ? AND painel = ?", agora, painelFiltro)
	} else {
		res, _ = db.Exec("DELETE FROM clientes WHERE exp_date > 0 AND exp_date < ?", agora)
	}
	n, _ := res.RowsAffected()
	invalidarAuthCache()
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "deletados": n})
}

func apiCriarUsuarioHandler(w http.ResponseWriter, r *http.Request) {
	if !verificarAdmin(w, r) { return }
	w.Header().Set("Content-Type", "application/json")
	user := r.URL.Query().Get("username")
	pass := r.URL.Query().Get("password")
	painel := r.URL.Query().Get("painel")
	maxCons, _ := strconv.Atoi(r.URL.Query().Get("max_connections"))
	dias, _ := strconv.Atoi(r.URL.Query().Get("dias"))
	isTrial, _ := strconv.Atoi(r.URL.Query().Get("is_trial"))
	bouquet := r.URL.Query().Get("bouquet")
	if user == "" || pass == "" || painel == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "erro": "Preencha todos os campos"})
		return
	}
	if maxCons == 0 { maxCons = 1 }
	if bouquet == "" { bouquet = "COMPLETO C/ ADULTOS" }
	var expUnix int64 = 0
	if dias > 0 {
		expUnix = time.Now().Unix() + int64(dias)*86400
	} else if isTrial == 1 {
		expUnix = time.Now().Unix() + 4*3600
	}
	agora := time.Now().Unix()
	_, err := db.Exec("INSERT INTO clientes (username, password, exp_date, max_connections, bouquet, created_at, painel, enabled, is_trial) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)",
		user, pass, expUnix, maxCons, bouquet, agora, painel, isTrial)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "erro": "Usuario ja existe"})
		return
	}
	invalidarAuthCache()
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "username": user, "password": pass})
}

func apiDeletePainelHandler(w http.ResponseWriter, r *http.Request) {
	if !verificarAdmin(w, r) { return }
	w.Header().Set("Content-Type", "application/json")
	painel := r.URL.Query().Get("painel")
	senha := r.URL.Query().Get("senha")
	if senha != Env.AdminPass {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "erro": "Senha incorreta"})
		return
	}
	res, _ := db.Exec("DELETE FROM clientes WHERE painel = ?", painel)
	n, _ := res.RowsAffected()
	invalidarAuthCache()
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "deletados": n})
}

func painelHandler(w http.ResponseWriter, r *http.Request) {
	if !verificarAdmin(w, r) { return }
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	totalConexoes, _ := getConexoesAtivas()
	dns := Env.AdminDNS
	if dns == "" { dns = "SEU_IP" }
	painelJSON, _ := json.Marshal(Env.SigmaPaineis)

	html := `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>GO IPTV Admin</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#0a0e14;color:#c9d1d9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif}
.wrap{max-width:1200px;margin:0 auto;padding:16px}
.header{display:flex;align-items:center;justify-content:space-between;margin-bottom:20px;padding-bottom:12px;border-bottom:1px solid #1a1f2b}
.header h1{color:#58a6ff;font-size:18px}
.header .info{color:#6e7681;font-size:12px}
.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(110px,1fr));gap:8px;margin-bottom:18px}
.stat{background:linear-gradient(135deg,#111820,#161f2b);border:1px solid #1f2937;border-radius:12px;padding:14px 10px;text-align:center;transition:.2s}
.stat:hover{border-color:#30404f;transform:translateY(-2px)}
.stat .n{font-size:26px;font-weight:700}.stat .l{font-size:10px;color:#6e7681;text-transform:uppercase;letter-spacing:.8px;margin-top:2px}
.green{color:#3fb950}.yellow{color:#d29922}.red{color:#f85149}.purple{color:#bc8cff}.blue{color:#58a6ff}
.section{background:#111820;border:1px solid #1f2937;border-radius:12px;padding:16px;margin-bottom:14px}
.section-title{font-size:13px;font-weight:600;color:#58a6ff;margin-bottom:10px}
.chips{display:flex;gap:5px;flex-wrap:wrap;margin-bottom:12px}
.chip{padding:5px 12px;border-radius:16px;font-size:11px;font-weight:600;cursor:pointer;border:1px solid #1f2937;background:#0a0e14;color:#6e7681;transition:.15s}
.chip:hover,.chip.active{background:#1f6feb;color:#fff;border-color:#1f6feb}
.chip .x{margin-left:6px;color:#f85149;cursor:pointer;font-weight:700}
.row{display:flex;gap:8px;flex-wrap:wrap;align-items:flex-end}
.field{display:flex;flex-direction:column;gap:3px}
.field label{font-size:10px;color:#6e7681;text-transform:uppercase;letter-spacing:.5px}
.field input,.field select{background:#0a0e14;border:1px solid #1f2937;color:#c9d1d9;padding:7px 10px;border-radius:8px;font-size:12px;min-width:100px}
.field input:focus,.field select:focus{border-color:#58a6ff;outline:none}
.btn{padding:7px 14px;border-radius:8px;border:none;cursor:pointer;font-size:11px;font-weight:600;transition:.15s;color:#fff}
.btn:hover{transform:translateY(-1px)}.btn:active{transform:scale(.97)}
.btn-blue{background:#1f6feb}.btn-red{background:#da3633}.btn-green{background:#238636}.btn-yellow{background:#9e6a03}
.btn-sm{padding:4px 8px;font-size:10px;border-radius:5px}
.btn-outline{background:transparent;border:1px solid #30404f;color:#8b949e}.btn-outline:hover{border-color:#58a6ff;color:#58a6ff}
.tw{overflow-x:auto;border-radius:10px;border:1px solid #1f2937}
table{width:100%;border-collapse:collapse;font-size:12px}
th{background:#0d1219;color:#6e7681;font-weight:600;text-transform:uppercase;font-size:10px;letter-spacing:.5px;padding:8px 10px;text-align:left;position:sticky;top:0}
td{padding:7px 10px;border-top:1px solid #141c26}
tr:hover td{background:#0f1620}
.badge{display:inline-block;padding:2px 8px;border-radius:10px;font-size:10px;font-weight:600}
.bg{background:#0f2d1a;color:#3fb950;border:1px solid #238636}
.by{background:#2d2a0f;color:#d29922;border:1px solid #9e6a03}
.br{background:#2d0f0f;color:#f85149;border:1px solid #da3633}
.bp{background:#1f0f2d;color:#bc8cff;border:1px solid #8957e5}
.bk{background:#141414;color:#6e7681;border:1px solid #30404f}
.user{font-family:'Courier New',monospace;color:#79c0ff;font-weight:600;font-size:12px}
.pass{font-family:'Courier New',monospace;color:#6e7681;cursor:pointer;font-size:11px}
.pass:hover{color:#c9d1d9}
.acoes{display:flex;gap:3px}
.link-btn{background:#0d1219;border:1px solid #1f2937;color:#58a6ff;padding:3px 7px;border-radius:5px;font-size:9px;cursor:pointer;transition:.15s}
.link-btn:hover{background:#1f6feb;color:#fff;border-color:#1f6feb}
.empty{text-align:center;padding:40px;color:#30404f;font-size:14px}
.count{color:#6e7681;font-size:11px;margin-bottom:6px;display:flex;align-items:center;gap:10px}
.pg{color:#58a6ff;cursor:pointer;font-weight:600;padding:4px 10px;border:1px solid #1f2937;border-radius:6px;font-size:11px}
.pg:hover{background:#1f6feb;color:#fff;border-color:#1f6feb}
.toast{position:fixed;bottom:20px;right:20px;padding:12px 20px;border-radius:10px;font-size:13px;font-weight:600;z-index:999;opacity:0;transition:.3s;pointer-events:none}
.toast.show{opacity:1}.toast.ok{background:#238636;color:#fff}.toast.error{background:#da3633;color:#fff}
.modal-bg{display:none;position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,.7);z-index:998;align-items:center;justify-content:center}
.modal-bg.show{display:flex}
.modal{background:#111820;border:1px solid #1f2937;border-radius:14px;padding:24px;min-width:320px;max-width:90vw}
.modal h3{color:#c9d1d9;font-size:15px;margin-bottom:14px}
.modal .field{margin-bottom:10px}
.modal .field input,.modal .field select{width:100%}
.modal .btns{display:flex;gap:8px;margin-top:14px;justify-content:flex-end}
</style></head><body>
<div class="wrap">
<div class="header">
 <div><h1>GO IPTV Server</h1><span class="info">` + fmt.Sprintf("%d conexoes | %d paineis | DNS: %s", totalConexoes, len(Env.SigmaPaineis), dns) + `</span></div>
 <button class="btn btn-blue" onclick="carregarTudo()">Atualizar</button>
</div>
<div class="stats" id="stats"></div>
<div class="section">
 <div class="section-title">Paineis Sigma</div>
 <div class="chips" id="paineis-bar"><div class="chip active" data-p="todos" onclick="filtrarPainel('todos')">Todos</div></div>
</div>
<div class="section">
 <div class="section-title">Ferramentas</div>
 <div class="row">
  <button class="btn btn-green" onclick="abrirModal('criar')">+ Criar Usuario</button>
  <button class="btn btn-yellow" onclick="abrirModal('teste')">+ Criar Teste</button>
  <button class="btn btn-red" onclick="deletarExpirados()">Limpar Expirados</button>
  <div class="field" style="flex:1"><input type="text" id="busca" placeholder="Buscar usuario..." oninput="debounceCarregar()"></div>
  <div class="field"><select id="filtro-status" onchange="carregarUsuarios()">
   <option value="todos">Todos</option><option value="ativo">Ativos</option><option value="teste">Testes</option><option value="expirado">Expirados</option><option value="bloqueado">Bloqueados</option>
  </select></div>
 </div>
</div>
<div class="count" id="count"></div>
<div class="tw"><table>
<thead><tr><th>ID</th><th>Usuario</th><th>Senha</th><th>Validade</th><th>Telas</th><th>Plano</th><th>Painel</th><th>Tipo</th><th>Status</th><th>Links</th><th>Acoes</th></tr></thead>
<tbody id="tbody"></tbody>
</table></div>
<div class="empty" id="empty" style="display:none">Nenhum usuario</div>
</div>

<div class="modal-bg" id="modal-bg" onclick="if(event.target===this)fecharModal()">
<div class="modal" id="modal"></div>
</div>
<div class="toast" id="toast"></div>

<script>
const PAINEIS=` + string(painelJSON) + `;
const DNS='` + dns + `';
let painelAtual='todos',debounceTimer,paginaAtual=1;

function debounceCarregar(){clearTimeout(debounceTimer);debounceTimer=setTimeout(()=>{paginaAtual=1;carregarUsuarios()},300)}
function toast(msg,ok){const t=document.getElementById('toast');t.textContent=msg;t.className='toast show '+(ok!==false?'ok':'error');setTimeout(()=>t.className='toast',2500)}

function filtrarPainel(p){painelAtual=p;paginaAtual=1;document.querySelectorAll('.chip').forEach(c=>c.classList.toggle('active',c.dataset.p===p));carregarUsuarios()}

function abrirModal(tipo){
 const m=document.getElementById('modal'),bg=document.getElementById('modal-bg');
 let opts=PAINEIS.map(p=>'<option value="'+p+'">'+p+'</option>').join('');
 if(tipo==='criar'){
  m.innerHTML='<h3>Criar Usuario</h3>'+
   '<div class="field"><label>Painel Sigma</label><select id="m-painel">'+opts+'</select></div>'+
   '<div class="field"><label>Username</label><input id="m-user" placeholder="deixe vazio = gerar auto"></div>'+
   '<div class="field"><label>Password</label><input id="m-pass" placeholder="deixe vazio = gerar auto"></div>'+
   '<div class="field"><label>Dias de validade</label><input id="m-dias" type="number" value="30"></div>'+
   '<div class="field"><label>Max telas</label><input id="m-telas" type="number" value="1"></div>'+
   '<div class="field"><label>Plano</label><select id="m-bouquet"><option value="COMPLETO C/ ADULTOS">C/ Adultos</option><option value="COMPLETO S/ ADULTOS">S/ Adultos</option></select></div>'+
   '<div class="btns"><button class="btn btn-outline" onclick="fecharModal()">Cancelar</button><button class="btn btn-green" onclick="criarUsuario(0)">Criar</button></div>';
 } else {
  m.innerHTML='<h3>Criar Teste (4 horas)</h3>'+
   '<div class="field"><label>Painel Sigma</label><select id="m-painel">'+opts+'</select></div>'+
   '<div class="field"><label>Username</label><input id="m-user" placeholder="deixe vazio = gerar auto"></div>'+
   '<div class="field"><label>Password</label><input id="m-pass" placeholder="deixe vazio = gerar auto"></div>'+
   '<div class="field"><label>Max telas</label><input id="m-telas" type="number" value="1"></div>'+
   '<div class="field"><label>Plano</label><select id="m-bouquet"><option value="COMPLETO C/ ADULTOS">C/ Adultos</option><option value="COMPLETO S/ ADULTOS">S/ Adultos</option></select></div>'+
   '<div class="btns"><button class="btn btn-outline" onclick="fecharModal()">Cancelar</button><button class="btn btn-yellow" onclick="criarUsuario(1)">Criar Teste</button></div>';
 }
 bg.classList.add('show');
}
function fecharModal(){document.getElementById('modal-bg').classList.remove('show')}

function gerarRand(){return Math.floor(10000000+Math.random()*90000000).toString()}

async function criarUsuario(isTrial){
 let user=document.getElementById('m-user').value||gerarRand();
 let pass=document.getElementById('m-pass').value||gerarRand();
 let painel=document.getElementById('m-painel').value;
 let telas=document.getElementById('m-telas').value||'1';
 let bouquet=document.getElementById('m-bouquet').value;
 let dias=isTrial?'0':(document.getElementById('m-dias')?document.getElementById('m-dias').value:'30');
 let url='/api/usuario/criar?username='+user+'&password='+pass+'&painel='+painel+'&max_connections='+telas+'&bouquet='+encodeURIComponent(bouquet)+'&dias='+dias+'&is_trial='+isTrial;
 try{const r=await fetch(url);const d=await r.json();
  if(d.ok){toast('Usuario '+user+' criado!');fecharModal();carregarTudo()}
  else{toast(d.erro||'Erro',false)}
 }catch(e){toast('Erro!',false)}
}

async function carregarStats(){
 try{const r=await fetch('/stats');const data=await r.json();
 let tA=0,tT=0,tE=0,tB=0,tAll=0;
 const bar=document.getElementById('paineis-bar');
 bar.innerHTML='<div class="chip'+(painelAtual==='todos'?' active':'')+'" data-p="todos" onclick="filtrarPainel(\'todos\')">Todos</div>';
 (data||[]).forEach(p=>{tA+=p.ativos||0;tT+=p.teste||0;tE+=p.expirados||0;tB+=p.bloqueados||0;tAll+=p.total||0;
  bar.innerHTML+='<div class="chip'+(painelAtual===p.painel?' active':'')+'" data-p="'+p.painel+'" onclick="filtrarPainel(\''+p.painel+'\')">'+p.painel+' ('+p.total+')<span class="x" onclick="event.stopPropagation();deletarPainel(\''+p.painel+'\')">X</span></div>';
 });
 document.getElementById('stats').innerHTML=
  '<div class="stat"><div class="n blue">'+tAll+'</div><div class="l">Total</div></div>'+
  '<div class="stat"><div class="n green">'+tA+'</div><div class="l">Ativos</div></div>'+
  '<div class="stat"><div class="n yellow">'+tT+'</div><div class="l">Testes</div></div>'+
  '<div class="stat"><div class="n red">'+tE+'</div><div class="l">Expirados</div></div>'+
  '<div class="stat"><div class="n purple">'+tB+'</div><div class="l">Bloqueados</div></div>';
 }catch(e){}
}

async function carregarUsuarios(){
 const search=document.getElementById('busca').value,status=document.getElementById('filtro-status').value;
 try{let url='/api/usuarios?painel='+painelAtual+'&status='+status+'&page='+paginaAtual;
 if(search)url+='&search='+encodeURIComponent(search);
 const r=await fetch(url);const resp=await r.json();
 const data=resp.data||[];const total=resp.total||0;const pages=resp.pages||1;
 const tbody=document.getElementById('tbody'),empty=document.getElementById('empty'),count=document.getElementById('count');
 if(!data||data.length===0){tbody.innerHTML='';empty.style.display='block';count.innerHTML='';return}
 empty.style.display='none';
 let pag='<span>'+total+' usuarios | Pagina '+paginaAtual+' de '+pages+'</span>';
 if(paginaAtual>1)pag+=' <span class="pg" onclick="irPagina('+(paginaAtual-1)+')">← Anterior</span>';
 if(paginaAtual<pages)pag+=' <span class="pg" onclick="irPagina('+(paginaAtual+1)+')">Proxima →</span>';
 count.innerHTML=pag;
 tbody.innerHTML=data.map(u=>{
  const isTeste=u.is_trial===1||u.is_trial==='1';
  let badge,statusTxt;
  if(u.enabled===0||u.enabled==='0'){badge='bp';statusTxt='Bloqueado'}
  else if(u.status==='expirado'){badge='br';statusTxt=isTeste?'Teste Exp':'Expirado'}
  else if(isTeste){badge='by';statusTxt='Teste'}
  else{badge='bg';statusTxt='Ativo'}
  const tipoTxt=isTeste?'<span class="badge by">Teste</span>':'<span class="badge bk">Pago</span>';
  const blockBtn=(u.enabled===1||u.enabled==='1')?'<button class="btn btn-sm btn-red" onclick="toggleUser('+u.id+',\'bloquear\')">Bloq</button>':'<button class="btn btn-sm btn-green" onclick="toggleUser('+u.id+',\'desbloquear\')">Ativar</button>';
  return '<tr><td>'+u.id+'</td>'+
   '<td><span class="user">'+u.username+'</span></td>'+
   '<td><span class="pass" onclick="copiar(\''+u.password+'\')">'+u.password+'</span></td>'+
   '<td>'+u.validade+'</td><td>'+u.max_connections+'</td>'+
   '<td><span class="badge bk">'+u.bouquet+'</span></td>'+
   '<td>'+u.painel+'</td><td>'+tipoTxt+'</td>'+
   '<td><span class="badge '+badge+'">'+statusTxt+'</span></td>'+
   '<td><span class="link-btn" data-link="'+u.link_m3u+'" onclick="copiar(this.dataset.link)">M3U</span> <span class="link-btn" data-link="'+u.link_hls+'" onclick="copiar(this.dataset.link)" style="background:#f0ad4e;color:#000">HLS</span></td>'+
   '<td><div class="acoes">'+blockBtn+'<button class="btn btn-sm btn-red" onclick="deletarUser('+u.id+',\''+u.username+'\')">X</button></div></td></tr>';
 }).join('');}catch(e){console.error(e)}
}
function irPagina(p){paginaAtual=p;carregarUsuarios();window.scrollTo(0,0)}

async function toggleUser(id,acao){try{await fetch('/api/usuario/toggle?id='+id+'&acao='+acao);toast(acao==='bloquear'?'Bloqueado':'Ativado');carregarTudo()}catch(e){toast('Erro',false)}}
async function deletarUser(id,u){if(!confirm('Deletar '+u+'?'))return;try{await fetch('/api/usuario/delete?id='+id);toast(u+' deletado');carregarTudo()}catch(e){toast('Erro',false)}}
async function deletarExpirados(){if(!confirm('Deletar TODOS expirados'+(painelAtual!=='todos'?' de '+painelAtual:'')+'?'))return;try{const r=await fetch('/api/usuario/limpar-expirados?painel='+painelAtual);const d=await r.json();toast(d.deletados+' removidos');carregarTudo()}catch(e){toast('Erro',false)}}
async function deletarPainel(p){
 const senha=prompt('Para deletar o painel '+p+' e TODOS seus usuarios, digite a senha admin:');
 if(!senha)return;
 try{const r=await fetch('/api/painel/delete?painel='+p+'&senha='+encodeURIComponent(senha));const d=await r.json();
  if(d.ok){toast('Painel '+p+' deletado! ('+d.deletados+' usuarios)');carregarTudo()}
  else{toast(d.erro||'Senha incorreta',false)}
 }catch(e){toast('Erro',false)}
}
function copiar(txt){navigator.clipboard.writeText(txt).then(()=>toast('Copiado!')).catch(()=>{
 const ta=document.createElement('textarea');ta.value=txt;document.body.appendChild(ta);ta.select();document.execCommand('copy');document.body.removeChild(ta);toast('Copiado!');
})}
function carregarTudo(){carregarStats();carregarUsuarios()}
carregarTudo();setInterval(carregarStats,15000);
</script></body></html>`
	w.Write([]byte(html))
}

func reResolveWebPlayers() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	var novos []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "WEBPLAYER") {
			if p := strings.SplitN(line, "=", 2); len(p) == 2 {
				itens := strings.Split(p[1], ",")
				for _, item := range itens {
					item = strings.TrimSpace(item)
					if item == "" {
						continue
					}
					if !isIPAddress(item) {
						ips, err := net.LookupHost(item)
						if err == nil {
							novos = append(novos, ips...)
						}
						novos = append(novos, item)
					} else {
						novos = append(novos, item)
					}
				}
			}
		}
	}
	if len(novos) > 0 {
		Env.WebPlayers = novos
	}
}

func main() {
	os.Setenv("TZ", "America/Sao_Paulo")
	time.Local, _ = time.LoadLocation("America/Sao_Paulo")
	fmt.Printf("🕐 Timezone: %s | Hora local: %s\n", time.Local.String(), time.Now().Format("02/01/2006 15:04:05"))

	carregarEnv()
	iniciarBanco()

	carregarIPsRegistrados()
	carregarIPsBloqueados()

	transporteOtimizado := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 50,
		MaxConnsPerHost:     100,
		IdleConnTimeout:     120 * time.Second,
		DisableCompression:  false,
		ForceAttemptHTTP2:   true,
	}
	httpClient = &http.Client{Transport: transporteOtimizado, Timeout: 60 * time.Second}
	clientAcelerado = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 30,
			IdleConnTimeout:     60 * time.Second,
		},
		Timeout:       25 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}

	streamClient = &http.Client{
		Transport: transporteOtimizado,
		Timeout:   0,
	}

	fmt.Println("=====================================================================")
	fmt.Println("🚀 [GO IPTV SERVER V18: MULTI-PAINEL SIGMA]")
	fmt.Printf("   📺 Formato Canal: %s\n", Env.FormatoCanal)
	fmt.Printf("   🔵 Painéis Sigma: %d registrados\n", len(Env.SigmaPaineis))
	for i, p := range Env.SigmaPaineis {
		fmt.Printf("      %d. /%s\n", i+1, p)
	}
	if len(Env.WebPlayers) > 0 {
		fmt.Printf("   🌐 WebPlayers: %d confiáveis\n", len(Env.WebPlayers))
	}
	fmt.Println("   🌐 Controle de IP/Telas: 30min TTL")
	fmt.Println("   🛡️ Log de IPs: ips_clientes.txt | Bloqueio: ips_bloqueados.txt")
	fmt.Println("   ⚡ Pool de conexões | GZIP | Auth Cache | SQLite WAL")
	fmt.Printf("   🖥️  Painel Admin: http://SEU_IP/painel (login: %s)\n", Env.AdminUser)
	if Env.AdminDNS != "" {
		fmt.Printf("   🌐 DNS: %s\n", Env.AdminDNS)
	}
	fmt.Println("=====================================================================")
	mostrarBancoNoTerminal()
	mostrarEstatisticasPaineis()

	go atualizarTudo()
	go func() {
		for {
			time.Sleep(24 * time.Hour)
			atualizarTudo()
		}
	}()
	go limparAuthCache()
	go limparIPsExpirados()
	go monitorarIPsBloqueados()
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			mostrarEstatisticasPaineis()
		}
	}()

	go func() {
		for {
			time.Sleep(30 * time.Minute)
			reResolveWebPlayers()
		}
	}()

	for _, painel := range Env.SigmaPaineis {
		path := "/" + painel
		http.HandleFunc(path+"/", sigmaHandler)
		http.HandleFunc(path, sigmaHandler)
		fmt.Printf("   ✅ Sigma registrado: %s\n", path)
	}

	http.HandleFunc("/player_api.php", xtreamAPIHandler)
	http.HandleFunc("/xmltv.php", xtreamAPIHandler)
	http.HandleFunc("/get.php", gerarListaM3U)
	http.HandleFunc("/stats", statsHandler)
	http.HandleFunc("/painel", painelHandler)
	http.HandleFunc("/api/usuarios", apiUsuariosHandler)
	http.HandleFunc("/api/usuario/toggle", apiToggleHandler)
	http.HandleFunc("/api/usuario/delete", apiDeleteHandler)
	http.HandleFunc("/api/usuario/criar", apiCriarUsuarioHandler)
	http.HandleFunc("/api/usuario/limpar-expirados", apiDeleteExpiredHandler)
	http.HandleFunc("/api/painel/delete", apiDeletePainelHandler)
	http.HandleFunc("/", playHandler)

	server := &http.Server{
		Addr:         PortaGo,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}
	fmt.Printf("\n🚀 Servidor rodando na porta %s\n\n", PortaGo)
	log.Fatal(server.ListenAndServe())
}
