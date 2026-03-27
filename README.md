Com certeza! Ter uma documentação clara é o segredo para resolver problemas rápido quando os clientes começam a reclamar. 

Preparei um `README.md` completo, detalhado e estruturado. Ele divide o seu código em "módulos" e explica o que cada função faz, além de incluir **observações de ouro (Troubleshooting)** para você saber exatamente onde procurar quando der algum problema no cliente final.

Copie o texto abaixo e salve como `README.md` na pasta do seu projeto.

---

# 📖 Documentação Oficial - GO IPTV SERVER V18

Este documento detalha o funcionamento interno do sistema, escrito em Go. Ele atua como um "Proxy Inteligente" entre o painel Master (sua fonte) e os clientes finais, gerenciando conexões, filtrando conteúdos, fornecendo integração com painéis Sigma e servindo um painel administrativo local.

---

## 🏗️ 1. Arquitetura Geral do Sistema
O sistema não hospeda os vídeos. Ele faz o "meio de campo":
1. O cliente pede o canal/filme para o seu servidor.
2. Seu servidor verifica se o cliente pagou, se não estourou o limite de telas e se o IP está liberado.
3. Estando tudo OK, o seu servidor busca o vídeo na Fonte (Master) e repassa para o cliente em tempo real.

---

## ⚙️ 2. Módulo de Configuração e Banco de Dados

### `carregarEnv()`
* **O que faz:** Lê o arquivo `.env` e carrega todas as senhas da fonte, DNS, formatos de canais, WebPlayers confiáveis, categorias que devem ser ocultadas e a ordem das categorias.
* **💡 Observação para problemas:** Se categorias indesejadas estiverem aparecendo ou se o sistema não conectar na fonte, o problema geralmente está em erros de digitação no arquivo `.env`.

### `iniciarBanco()` e `mostrarBancoNoTerminal()`
* **O que faz:** Cria (se não existir) e conecta ao banco de dados local `usuarios.db` usando SQLite (modo WAL para alta performance). A segunda função imprime a lista de usuários no terminal quando o servidor é iniciado.

---

## 🛡️ 3. Módulo de Segurança, Autenticação e Telas

### `validarUsuarioSQLite()` e `validarUsuarioCache()`
* **O que faz:** Verifica no banco de dados se o `username` e `password` do cliente estão corretos, se a conta não expirou e se está habilitada (`enabled = 1`). O `Cache` guarda essa resposta por 30 segundos para não sobrecarregar o banco de dados se o app do cliente ficar pedindo login repetidamente.
* **💡 Observação:** O sistema tem uma regra secreta (`maxCons = maxCons + 1`). Ele dá **1 tela de tolerância** invisível para evitar que apps muito "gulosos" (que abrem 2 conexões rápidas para carregar o EPG e o vídeo) bloqueiem o cliente à toa.

### `verificarIP()` e `pegarIP()`
* **O que faz:** Lê o IP real do cliente. Conta quantos IPs diferentes aquele usuário usou nos últimos **60 minutos** (configurado na variável `ipTTL = 1 * time.Hour`).
* **⚠️ Se o cliente relatar "Erro de Limite de Telas":** Verifique o terminal. Se ele conectou na TV e no Celular (2 IPs) tendo um plano de 1 tela, essa função vai barrar ele e retornar Erro 403.

### `limparIPsExpirados()`
* **O que faz:** Roda em background a cada 5 minutos, apagando da memória os IPs que não fazem nenhuma requisição há mais de 1 hora.

### `carregarIPsBloqueados()` e `ipBloqueado()`
* **O que faz:** Lê o arquivo texto `ips_bloqueados.txt`. Se um IP estiver lá, o sistema corta a conexão na hora com `Erro 403`, sem nem checar usuário e senha. Excelente para banir hackers, curiosos ou concorrentes.

---

## 🔄 4. Módulo da "Matriz" (Sincronização com a Fonte)

### `atualizarTudo()`
* **O que faz:** É o coração da sincronização. Puxa as categorias, canais, filmes e séries da Fonte Master. Ele roda quando o servidor liga e depois **a cada 6 horas** em background. Salva tudo na memória RAM (`ApiCacheCompleto` e `ApiCacheLivre`).
* **⚠️ Se os canais sumirem ou a lista ficar vazia:** Significa que no momento exato em que essa função rodou, a sua Fonte Master estava fora do ar. Reinicie o seu servidor Go para forçar ele a puxar a lista de novo.

### `fetchAndFilterCategories()` e `fetchAndFilterStreams()`
* **O que faz:** Formata o JSON original da Master. Renomeia categorias, aplica a ordem customizada e cria uma versão da lista com conteúdo adulto e outra sem (para os clientes de pacotes familiares).
* **💡 Observação Anti-Bloqueio:** Injeta o User-Agent `IPTV Smarters Pro` para a sua Master achar que é um aplicativo acessando, e não um servidor proxy puxando a lista inteira.

### `baixarEFiltrarM3U()`
* **O que faz:** Faz a mesma coisa que as funções acima, mas especificamente para os links brutos `.m3u` usados em Smart TVs mais antigas (SS IPTV, Smart STB).

---

## 🌐 5. Módulo de Endpoints (Acesso do Cliente)

### `xtreamAPIHandler()` (Acesso via XUI / Smarters / XCIPTV)
* **O que faz:** Responde ao link `/player_api.php`. É aqui que 90% dos aplicativos se conectam. Ele devolve o JSON do cache, substituindo o link da Master pelo **seu DNS**.
* **💡 Sacada Genial:** Injeta o `{EXT_LIVE}`. Se a função `isAppAntigo()` detectar um app problemático (ex: VLC, Smart STB), ele injeta `.ts` no final do link. Se for um app moderno, ele deixa sem extensão (padrão XUI atual) para despistar bloqueios de operadoras.

### `gerarListaM3U()` (Acesso via link M3U)
* **O que faz:** Responde ao `/get.php`. Monta a lista de texto puro, injeta as senhas e compacta em `GZIP` se o app suportar (isso economiza MUITA internet do seu servidor e o arquivo baixa na hora).

### `playHandler()` e `proxyStream()` (O PLAY DO VÍDEO)
* **O que faz:** Responde a `/live/`, `/movie/` e `/series/`. Pega o pedido do cliente, vai na Master (usando o `clientAcelerado`), pega o vídeo e envia de volta para o cliente como um tubo contínuo.
* **⚠️ Se a tela ficar preta e não carregar:** O `clientAcelerado` está configurado com `Timeout: 25 * time.Second`. Se a Master demorar mais de 25 segundos para entregar o vídeo, essa função corta e dá "Erro 500".
* **💡 ProxyStream 206:** Esta função foi muito bem feita pois repassa cabeçalhos como `Range` e `Content-Range`. Isso permite que o cliente pule o filme para frente/trás (Fast Forward) perfeitamente.

---

## 🤖 6. Módulo Sigma e API Painel Administrativo

### `sigmaHandler()`
* **O que faz:** Simula um painel "Sigma" oficial. Responde a ações como `create_line`, `delete_line`, `get_lines`. Isso permite que você conecte softwares/robôs de revenda de terceiros apontando para o seu servidor.
* **💡 Trava de Segurança:** Tem um bloqueio que impede a criação de contas via Sigma com mais de **2 conexões** de propósito, para evitar que revendedores criem contas de 100 telas e derrubem a sua master.

### `painelHandler()` e Rotas `/api/...`
* **O que faz:** Entrega o código HTML/JS e processa os dados do seu painel administrativo visual acessado pelo navegador (para você criar testes, bloquear usuários e ver estatísticas).

---

## 🚦 7. Goroutines (Tarefas em Segundo Plano)
Na função `main()`, o sistema dispara vários "trabalhadores" silenciosos:
1. `go atualizarTudo()` -> Atualiza o VOD/Canais.
2. `go limparAuthCache()` -> Limpa senhas cacheadas.
3. `go limparIPsExpirados()` -> Libera as telas presas de clientes.
4. `go monitorarIPsBloqueados()` -> Lê o `ips_bloqueados.txt` a cada 10 segundos para você poder banir pessoas em tempo real sem precisar reiniciar o servidor.
5. `go reResolveWebPlayers()` -> Atualiza o IP dos painéis web para não bloqueá-los se a Cloudflare mudar o IP deles.

---

## 🛠️ Guia Rápido de Solução de Problemas (Troubleshooting)

| Sintoma relatado pelo cliente | Causa Provável no Código | Onde investigar / O que fazer |
| :--- | :--- | :--- |
| **"Deu Erro de Login / Senha Incorreta"** | Usuário desativado ou senha errada. | Verifique no Painel Web se o cliente está ativo (`enabled = 1`). Se recém-criado, aguarde uns segundos devido ao `AuthCache`. |
| **"Aplicativo acusa Limite de Telas"** | Cliente conectou em vários IPs recentemente. | Verifique o terminal para ver o print: `🚫 BLOQUEADO: [user] IP [X]`. A variável `ipTTL` segura o IP por 1 hora. Peça ao cliente para desconectar o outro aparelho e aguardar. |
| **"Categorias VOD/Canais sumiram!"** | A Fonte falhou na hora da sincronização. | A função `atualizarTudo()` pegou um JSON vazio da Master. Reinicie o Servidor GO via terminal para ele repuxar tudo imediatamente. |
| **"O canal tenta carregar e a tela fica preta"** | Lentidão extrema na Fonte Master ou no seu Proxy. | O `clientAcelerado` esgotou os 25 segundos. Teste o link da Master direto no seu celular. Se demorar mais de 25s, o problema é na fonte. |
| **"Filmes (VOD) não avançam ou dão erro"** | O App do cliente não entende o `proxyStream`. | Veja se o cliente está usando um aplicativo antigo. A transferência em blocos (Range) às vezes falha em TVs antigas (SS IPTV). |

---
**Fim da Documentação**
