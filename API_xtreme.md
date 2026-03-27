Tranquilo, mano! Já deixei todas as URLs prontas com os seus usuários e senhas pra você só copiar, colar e testar no seu navegador ou no Postman. 

Separei as rotas em **Canais (LIVES e EPG)** e **VODs (Filmes e Séries)** de acordo com as contas que você me mandou. Onde a API pede um ID específico (categorias, infos, etc.), eu deixei a marcação `<COLOQUE_O_ID_AQUI>` pra você substituir pelo número que quiser testar.

---

### 📺 CANAIS, LIVES e EPG (Conta `Xtreme8749_783`)

**Geral (Lista Completa M3U):**
* `http://vods.site/get.php?username=Xtreme8749_783&password=951528472&type=m3u_plus&output=ts`

**Lives:**
* **Todas as Lives:** `http://vods.site/player_api.php?username=Xtreme8749_783&password=951528472&action=get_live_streams`
* **Todas as Categorias:** `http://vods.site/player_api.php?username=Xtreme8749_783&password=951528472&action=get_live_categories`
* **Lives de uma Categoria Específica:** `http://vods.site/player_api.php?username=Xtreme8749_783&password=951528472&action=get_live_streams&category_id=<COLOQUE_O_ID_AQUI>`

**EPG (Guia de Programação):**
* **EPG Completo (Todos os Canais):** `http://vods.site/xmltv.php?username=Xtreme8749_783&password=951528472`
* **EPG Curto de um Canal Específico:** `http://vods.site/player_api.php?username=Xtreme8749_783&password=951528472&action=get_short_epg&stream_id=<COLOQUE_O_ID_AQUI>`
* **EPG Completo de um Canal Específico:** `http://vods.site/player_api.php?username=Xtreme8749_783&password=951528472&action=get_simple_date_table&stream_id=<COLOQUE_O_ID_AQUI>`

---

### 🎬 VODS, FILMES e SÉRIES (Conta `snux-vodshdk485@jdjdks`)

> **Nota importante:** O seu usuário de VOD tem um arroba (`@`). Se alguma rota der erro na hora de testar no navegador, tente trocar o `@` por `%40` (que é o código URL para arroba). Exemplo: `snux-vodshdk485%40jdjdks`.

**Filmes (VOD):**
* **Todos os Filmes:** `http://vods.site/player_api.php?username=snux-vodshdk485@jdjdks&password=09dhdusnux47583&action=get_vod_streams`
* **Categorias de Filmes:** `http://vods.site/player_api.php?username=snux-vodshdk485@jdjdks&password=09dhdusnux47583&action=get_vod_categories`
* **Filmes de uma Categoria Específica:** `http://vods.site/player_api.php?username=snux-vodshdk485@jdjdks&password=09dhdusnux47583&action=get_vod_streams&category_id=<COLOQUE_O_ID_AQUI>`
* **Informações de um Filme Específico:** `http://vods.site/player_api.php?username=snux-vodshdk485@jdjdks&password=09dhdusnux47583&action=get_vod_info&vod_id=<COLOQUE_O_ID_AQUI>`

**Séries:**
* **Todas as Séries:** `http://vods.site/player_api.php?username=snux-vodshdk485@jdjdks&password=09dhdusnux47583&action=get_series`
* **Categorias de Séries:** `http://vods.site/player_api.php?username=snux-vodshdk485@jdjdks&password=09dhdusnux47583&action=get_series_categories`
* **Séries de uma Categoria Específica:** `http://vods.site/player_api.php?username=snux-vodshdk485@jdjdks&password=09dhdusnux47583&action=get_series&category_id=<COLOQUE_O_ID_AQUI>`
* **Informações de uma Série Específica:** `http://vods.site/player_api.php?username=snux-vodshdk485@jdjdks&password=09dhdusnux47583&action=get_series_info&series=<COLOQUE_O_ID_AQUI>`

---

Tudo pronto pra você testar! Você quer que eu monte um script rápido em Python ou JavaScript pra testar todas essas rotas de uma vez e te trazer os resultados?
