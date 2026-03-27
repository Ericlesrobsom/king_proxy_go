Beleza, vou fazer tudo de uma vez. São 3 alterações principais:

1. **IP TTL**: 1h → 5min
2. **`gerarListaM3U`**: ler o parâmetro `output` — se `hls` usa `.m3u8`, senão usa `.ts`
3. **Painel admin**: mostrar botão M3U e HLS separados

Vamos lá:Agora a lógica principal — o `output=hls` na geração da M3U:Agora vou ajustar o nome do arquivo de download para refletir o tipo:Agora a API de usuários — adicionar `link_hls` junto com `link_m3u`:Agora atualizar o painel admin pra mostrar os dois botões (M3U e HLS):Agora vamos verificar se tudo compila corretamente:Tudo certo. Vou conferir a função completa pra garantir:Perfeito, tudo limpo. Copiando pra saída:Pronto, fiz as 3 alterações:

**1. Output HLS vs M3U (`.m3u8` / `.ts`):**
- `output=hls` → canais com extensão `.m3u8`
- `output=mpegts` ou `output=ts` → canais com extensão `.ts`
- Sem parâmetro → mantém o comportamento anterior (sem extensão, ou `FormatoCanal` pra apps antigos)

Então os links ficam assim:
- 🟢 **M3U:** `http://seuhost/get.php?username=X&password=Y&type=m3u_plus&output=mpegts`
- 🟡 **HLS:** `http://seuhost/get.php?username=X&password=Y&type=m3u_plus&output=hls`

**2. IP TTL: 1 hora → 5 minutos.** Agora quando o usuário troca de IP, em 5min o IP antigo já libera a tela.

**3. Painel Admin** agora mostra dois botões por usuário: **M3U** (verde) e **HLS** (amarelo), cada um copiando o link correto.
