# Models for local LLM tools: formats, protocols, and what Quader would need

Research date: 2026-11-27. The clients were read at these points:
- `huggingface_hub` `main` (2.6.0.dev0; the latest release is v2.5.0)
- Ollama `main` at `8ae8acb8` (2026-11-26) and the release v0.40.18
- llama.cpp `master` at `6d8a8886`
- `lms` at `cdf919fc`
- Docker Model Runner, ModelPack, KitOps and the other repositories at their default branch on that day

Live requests were anonymous and read-only, and went to public endpoints only (huggingface.co, hf.co, registry.ollama.ai, registry-1.docker.io).

**Where the repository stands.**
- The owner decided on 2026-11-28 that local LLM models are the first topic after the beta features: block L of
  [FULL.md](FULL.md#l--local-llm-models-first-after-the-beta-features), in the order of section 8. This page is the research behind it.
- The catalogue pages [huggingface.md](../features/formats/huggingface.md) (P2) and [ml-models.md](../features/formats/ml-models.md)
  describe the formats at the level of client operations.
- When the research was done, Ollama, GGUF and OCI model artifacts appeared nowhere in the plan or the catalogue.

**Labels used throughout.**

| Label | Meaning |
|---|---|
| `[V-code]` | seen in the client's source |
| `[V-doc]` | official documentation |
| `[V-live]` | seen in a live response on 2026-11-27 |
| `[V-issue]` | a GitHub issue or PR |
| `[V-spec]` | Quader's own spec in this repository |
| `[I]` | an inference; a conclusion, not a fact |
| `[NV]` | checked and not settled |

---

## 0. Short answers

1. **Which formats do local LLMs need?** Two weight formats carry nearly all of it:
   - **GGUF:** one self-contained file per model or quantisation. Used by the llama.cpp family: Ollama, LM Studio, Jan, GPT4All, KoboldCpp, LocalAI, llamafile.
   - **safetensors with its side files** (`config.json`, tokenizer files, chat template). Used by transformers, vLLM and SGLang. Apple **MLX** is safetensors with an MLX quantisation block.
   - GPTQ, AWQ, EXL2 and EXL3 are safetensors with a `quantization_config`.
   - PyTorch pickle `.bin`, ONNX and OpenVINO are minor.

   For a repository all of these are opaque large files addressed by SHA-256. **The real question is the distribution protocol, not the file format.**

2. **Hugging Face: one protocol or several?** One protocol, with several file formats inside it. Quader would implement one format: the Hub HTTP API (model info, tree, refs, and `resolve` with the Hub's headers). It carries GGUF, safetensors, MLX and ONNX alike. The protocol has four layers:
   - the plain HTTP read path, which is enough for a caching proxy;
   - the **Xet** storage protocol: optional for reads up to 50 GB per file, effectively required for uploads with current clients;
   - the upload API: preupload, an NDJSON commit, and LFS batch or Xet;
   - the **Ollama-compatible `/v2/` view** that hf.co builds on the fly from GGUF repos.

3. **How do Ollama and LM Studio get models?**
   - **Ollama** speaks a registry protocol close to OCI Distribution. It differs in these ways:
     - Manifests are Docker v2 schema 2 with Ollama's own layer media types: `application/vnd.ollama.image.model` is the GGUF, plus `.template`, `.params` and others.
     - It has its own manifest-list type.
     - It has its own token handshake, signed with an ed25519 key.
     - A blob GET must answer with a `Location`.
     - Names are exactly `host/namespace/model:tag`.

     A plain OCI registry can *hold* Ollama models, but cannot serve them to Ollama without changes.
   - **LM Studio** downloads GGUF and MLX straight from huggingface.co through its closed-source app. It ignores `HF_ENDPOINT` and has no documented mirror or proxy setting. The only way to feed it is to sideload files (`lms import`).

4. **Further formats not to forget:**
   - **OCI model artifacts:** Docker Model Runner's `application/vnd.docker.ai.*` types, and **CNCF ModelPack**, which Docker Hub's `ai/` namespace already ships. Also **KitOps ModelKit**, ORAS artifacts and KServe modelcars. Quader's existing OCI format already accepts all of them, except that the blob sizes exceed its defaults.
   - **llama.cpp `-hf`** uses the Hub API and honours `MODEL_ENDPOINT` or `HF_ENDPOINT`.
   - **ModelScope** has its own API (`MODELSCOPE_ENDPOINT`). It matters in China and as a source for vLLM and NIM.
   - **Plain HTTP files** (llamafile, KoboldCpp, `llama.cpp -mu`) are served by the generic format.
   - Kaggle, NGC, PyTorch Hub, TF Hub, MLflow and Triton are not worth a format for local LLM use.

**Recommendation in brief** (details in section 8):
1. Hugging Face remote, read path without Xet.
2. An Ollama format of its own, built on the OCI format's internals, plus the Ollama view on the Hugging Face format.
3. Hugging Face local with uploads, which in practice means the Xet write path.
4. OCI model artifacts need no new format, only limits sized for models and client tests.
5. ModelScope only on demand.

LM Studio, Jan and GPT4All cannot be pointed at a mirror. Nothing Quader builds reaches them except files sideloaded by hand.

---

## 1. File formats of the weights and their side files (question 1)

| Format | What it is | Side files a client needs | Used by | Where it is distributed | Source |
|---|---|---|---|---|---|
| **GGUF** | One binary: magic `GGUF`, version 3, metadata key-values (`general.architecture` is required; also `tokenizer.ggml.*` and `tokenizer.chat_template` as Jinja), then the tensors. Quant types Q4_0…Q8_K, IQ*, TQ*, MXFP4 and others. Large models are split as `…-00001-of-0000N.gguf` (`split.no`, `split.count`) | none; optionally an `mmproj-*.gguf` (vision projector) | llama.cpp, Ollama, LM Studio, Jan, GPT4All, KoboldCpp, LocalAI, llamafile, Docker Model Runner (llama.cpp engine) | HF repos (`*-GGUF`); the Ollama registry (as the layer `application/vnd.ollama.image.model`); Docker Hub `ai/` (`application/vnd.docker.ai.gguf.v3`, or a ModelPack raw weight) | [ggml docs/gguf.md](https://github.com/ggml-org/ggml/blob/master/docs/gguf.md) [V-doc]; [HF GGUF](https://huggingface.co/docs/hub/gguf) [V-doc] |
| **safetensors** | An 8-byte little-endian header length, a JSON header `{name:{dtype,shape,data_offsets}, __metadata__}`, then the raw tensors. Loading executes no code | `config.json`, `generation_config.json`, `tokenizer.json`, `tokenizer_config.json` (carries `chat_template`), `chat_template.jinja`, `special_tokens_map.json`, sentencepiece `tokenizer.model`. Shards are indexed by `model.safetensors.index.json` (`weight_map`); that index is a transformers convention, not part of the format | transformers, diffusers, vLLM, SGLang, TGI, mlx_lm, Ollama (`ollama create` from a directory, MLX runner), Docker Model Runner (vLLM engine) | HF repos; Docker Hub `ai/…-safetensors` (ModelPack) | [safetensors README](https://github.com/huggingface/safetensors) [V-doc]; side files [V-code] in transformers and [V-live] in Qwen2.5 repos |
| **MLX** | safetensors plus a `config.json` block `quantization{group_size,bits,mode}` | as safetensors | LM Studio on Apple Silicon ([mlx-engine](https://github.com/lmstudio-ai/mlx-engine)), mlx_lm, Jan's MLX extension, Ollama's MLX runner | HF `mlx-community`; Docker Hub `ai/…-mlx` (ModelPack) | [mlx_lm utils.py](https://github.com/ml-explore/mlx-lm/blob/main/mlx_lm/utils.py) [V-code]; Docker Hub manifest [V-live] |
| **GPTQ, AWQ, EXL2, EXL3, bitsandbytes, compressed-tensors** | safetensors with `quantization_config.quant_method` in `config.json`. **EXL2 and EXL3 keep one git branch per bitrate** (`4.0bpw`, `6.0bpw`, …) | as safetensors | transformers, vLLM, TGI, exllamav2/v3 | HF repos, including branches other than `main` | [transformers quantization overview](https://huggingface.co/docs/transformers/main/en/quantization/overview) [V-doc]; EXL2 branches [V-live] |
| **PyTorch `.bin`/`.pt` (pickle)** | A pickled state dict; loading it can execute code | as safetensors | older transformers checkpoints, PyTorch Hub | older HF repos | [HF pickle security](https://huggingface.co/docs/hub/security-pickle) [V-doc] |
| **ONNX** | A `*.onnx` graph plus `*.onnx.data` external weights; `genai_config.json` for onnxruntime-genai | tokenizer files | onnxruntime-genai, Windows/NPU runtimes, Foundry Local | HF (`onnx-community`, `microsoft/*-onnx`). The ONNX Model Zoo moved to HF `onnxmodelzoo` | [genai config](https://onnxruntime.ai/docs/genai/reference/config.html) [V-doc]; [onnx/models](https://github.com/onnx/models) [V-doc] |
| **OpenVINO IR** | `openvino_model.xml` + `.bin` | tokenizer files | OpenVINO GenAI | HF `OpenVINO/*-ov` | HF API [V-live, by a sub-researcher] |
| **Ollama side layers** | `.template` (Go template), `.params` (JSON), `.system`, `.license`, `.messages`, `.adapter` (LoRA), `.projector`, `.embed`; `.tensor` and `.json` for safetensors/MLX models (one tensor per blob); `.draft` | — | Ollama | registry.ollama.ai; generated on the fly by hf.co | [manifest/layer.go](https://github.com/ollama/ollama/blob/main/manifest/layer.go) [V-code] |
| **Modelfile** | Ollama's build recipe (`FROM`, `TEMPLATE`, `PARAMETER`, `SYSTEM`, `ADAPTER`, `LICENSE`, `MESSAGE`, `REQUIRES`, …). It is not distributed as a file; its content becomes the layers above | — | `ollama create` | — | [parser/parser.go](https://github.com/ollama/ollama/blob/main/parser/parser.go) [V-code]; [docs/modelfile.mdx](https://github.com/ollama/ollama/blob/main/docs/modelfile.mdx) [V-doc] |
| **llamafile** | An executable (APE format) with the GGUF and `.args` inside a ZIP | — | llamafile | HF, GitHub releases | [creating_llamafiles.md](https://github.com/mozilla-ai/llamafile/blob/main/docs/creating_llamafiles.md) [V-doc] |

**What follows for a repository [I].** None of these formats has to be parsed to store or serve it. Parsing helps only in two places:
- **Display:** GGUF metadata gives architecture, quantisation, context length and chat template. The HF API itself returns a `gguf` object for GGUF repos [V-live].
- **Safety:** pickle files execute code on load. A pickle scan is the meaningful check for them, not ClamAV.

**Sizes in practice.**
- Single files of 4–65 GB are normal [V-live, by a sub-researcher]:
  - Docker Hub's `ai/gpt-oss:120b-f16` is one raw GGUF layer of 65,369,017,728 bytes.
  - `ai/gpt-oss:120B-UD-Q4_K_XL` is two GGUF layers of 49.9 GB and 13.1 GB.
- Hugging Face recommends files under 200 GB and enforces 500 GB per file ([storage limits](https://huggingface.co/docs/hub/storage-limits)) [V-doc].
- Neither ModelPack nor Cog splits files, so the largest blob is the largest weight file [V-doc].

---

## 2. Hugging Face: what has to be mastered (question 2)

**One protocol family, not one per file format.** GGUF, safetensors, MLX, ONNX and the side files are all plain files of a Hub repository, served by the same API. What has to be mastered is that API with its revision model, and optionally Xet.

### 2.1 The layers of the Hub protocol

| Layer | What it is | Remote (caching proxy) | Local (push) | Clients |
|---|---|---|---|---|
| Read API | `/api/models/{repo}[/revision/{rev}]`, `/tree/{rev}[/{path}]`, `/paths-info/{rev}`, `/refs`, `HEAD`/`GET /{repo}/resolve/{rev}/{file}` | **required** | **required** | all clients below |
| Xet read | `/api/models/{repo}/xet-read-token/{rev}` (answers with headers **and** a JSON body), CAS `/v2/reconstructions/{id}` (v1 fallback), xorb ranges | optional: if the mirror removes every Xet signal, clients use plain HTTP, up to 50 GB per file | optional | `huggingface_hub` with `hf_xet` (a hard dependency on x86_64 and arm64) |
| Upload API | `POST /api/repos/create`, `/preupload/{rev}`, `/commit/{rev}` (NDJSON), `/branch/{b}`, `/tag/{rev}`, `GET /api/whoami-v2` | — | **required** | `hf upload`, `upload_folder`, `push_to_hub` |
| Upload content | git-LFS batch `/{repo}.git/info/lfs/objects/batch` (basic, multipart, verify), **or** Xet write (`xet-write-token`, CAS xorbs and shards) | — | **Xet in practice:** since `huggingface_hub` 1.20.0 the client uploads through Xet whenever `hf_xet` is installed. LFS is used only with `HF_HUB_DISABLE_XET=1` | as above |
| git smart HTTP + LFS | `git clone https://…/{repo}` | optional | optional (`receive-pack` is a large extra surface) | git, git-lfs, git-xet |
| Ollama view | `GET /v2/{user}/{repo}/manifests/{quant}`, `HEAD`/`GET /v2/{user}/{repo}/blobs/{digest}` | optional; needed for `ollama pull <mirror>/{user}/{repo}:{quant}` | optional | Ollama, RamaLama, llama.cpp builds before 2026-03-24 |
| Datasets, Spaces | the same API under `/api/datasets/…` and `/datasets/{repo}/resolve/…` | not needed to run LLMs | not needed | datasets library |

Sources for the table:
- Code [V-code]: [constants.py](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/constants.py), [hf_api.py](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/hf_api.py), [file_download.py](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/file_download.py), [_commit_api.py `_upload_files`](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/_commit_api.py), [utils/_xet.py](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/utils/_xet.py), [setup.py](https://github.com/huggingface/huggingface_hub/blob/main/setup.py).
- Docs [V-doc]: [Xet API](https://huggingface.co/docs/xet/api), [Xet download protocol](https://huggingface.co/docs/xet/download-protocol), [Xet auth](https://huggingface.co/docs/xet/auth), [HF and Ollama](https://huggingface.co/docs/hub/ollama).
- The Ollama view and datasets: [V-live].

### 2.2 The read path in detail

| Fact | Tag and source |
|---|---|
| The base URL is `HF_ENDPOINT` (default `https://huggingface.co`). The file URL is `{ENDPOINT}/{repo_id}/resolve/{revision}/{filename}`, with `datasets/` and `spaces/` prefixes | [V-code] constants.py |
| transformers and diffusers download through `huggingface_hub` (`hf_hub_download`, `snapshot_download`), so `HF_ENDPOINT` covers them | [V-code] [transformers utils/hub.py](https://github.com/huggingface/transformers/blob/main/src/transformers/utils/hub.py), [diffusers hub_utils.py](https://github.com/huggingface/diffusers/blob/main/src/diffusers/utils/hub_utils.py) |
| The `HEAD` on `resolve` sends `Accept-Encoding: identity`. It reads `X-Repo-Commit`, `X-Linked-Etag` (else `ETag`), `X-Linked-Size` (else `Content-Length`), `Location`, and the Xet signals | [V-code] `get_hf_file_metadata`, file_download.py lines 1709–1718 |
| **A missing `X-Repo-Commit` is a hard error:** "Response from {url} is missing the 'X-Repo-Commit' header, so it does not seem to be served by a Hugging Face Hub endpoint". The client then uses its cache or fails. A response with no ETag at all is treated the same way | [V-code] file_download.py line 1824 |
| The ETag is the file's identity and the cache key: SHA-256 for LFS/Xet files, the git blob SHA-1 for files stored in git | [V-code] docstring of `hf_hub_url`. [V-live]: gpt2's `config.json` has `X-Linked-Etag` equal to the `/tree` `oid` (git SHA-1); its `model.safetensors` has `X-Linked-Etag` equal to `lfs.oid` (SHA-256) |
| Live `HEAD` of `openai-community/gpt2/resolve/main/model.safetensors`: `302` to `us.aws.cdn.hf.co/xet-bridge-us/…`; `X-Repo-Commit`; `X-Linked-Size: 548105171`; `X-Linked-Etag: "248dfc39…a707"`; `X-Xet-Hash: 63bed808…`; `Link: <…/xet-read-token/{commit}>; rel="xet-auth", <https://cas-server.xethub.hf.co/v1/reconstructions/…>; rel="xet-reconstruction-info"`; `ratelimit: "resolvers";r=2999;t=135` | [V-live] (my own request) |
| The CDN's own `ETag` on the Xet bridge is the Xet hash, not the SHA-256. That is why the client prefers `X-Linked-Etag` | [V-live, by a sub-researcher] |
| Redirects are followed only to the same origin or a Hub origin (`huggingface.co`, `hf.co`, `ENDPOINT`). A redirect to the CDN ends the HEAD. The client drops the token before it GETs a host that is not a Hub origin | [V-code] `_httpx2_follow_hub_redirects_with_backoff`, [utils/_http.py](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/utils/_http.py) |
| `snapshot_download` on `main` reads `repo_info.sha` (asserted to be 40 hex characters) and lists files with `/tree?recursive=true`, paging by cursor via `Link: rel="next"`. Versions v0.25–v0.36 read `siblings` from `/revision/{rev}` instead | [V-code] [_snapshot_download.py](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/_snapshot_download.py) at main and at tags v0.25.0 and v0.36.0 |
| `paths-info` is form-encoded (`paths=…&paths=…`), not JSON | [V-code] `get_paths_info`; [V-live] |
| transformers asks for `/tree/{rev}/additional_chat_templates`. Upstream answers 404 `EntryNotFound`, which a mirror must reproduce | [V-code] `list_repo_templates`; [V-live] |
| Downloads go in 10 MiB chunks, with up to 5 retries using `Range: bytes={n}-`. A 200 answer to a range request restarts the download. The final size is checked against the expected size | [V-code] `http_get` |
| **Plain HTTP refuses files over 50 GB:** `elif expected_size and expected_size > constants.MAX_HTTP_DOWNLOAD_SIZE: raise ValueError("… Install hf_xet …")`, with `MAX_HTTP_DOWNLOAD_SIZE = 50 * 1000 * 1000 * 1000` | [V-code] file_download.py line 380, constants.py line 41 (I read both) |
| There is no SHA-256 check after a download into the cache | [V-code] (SHA-256 is computed only in `local_dir` mode) |
| Errors are typed by `X-Error-Code`: `RepoNotFound`, `RevisionNotFound`, `EntryNotFound`, `GatedRepo`, `EgressLimitReached`. A gated repo without a token answers 401 `GatedRepo`. A missing file answers 404 `EntryNotFound` **with** `X-Repo-Commit`, and the client caches the absence under `.no_exist/{commit}/` | [V-code] `hf_raise_for_status`; [V-live] |
| `HF_HUB_OFFLINE` blocks every request. `HF_HUB_DISABLE_XET` forces plain HTTP and LFS. `HF_HUB_ENABLE_HF_TRANSFER` is deprecated in favour of `HF_XET_HIGH_PERFORMANCE`. The cache lives in `HF_HOME`/`HF_HUB_CACHE` with the layout `models--{org}--{name}/{blobs/{etag}, snapshots/{commit}/…, refs/{rev}}` | [V-code] constants.py, [utils/_runtime.py](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/utils/_runtime.py) |
| The `huggingface-cli` command is gone since v1.0.0. The CLI is `hf` (`hf download`, `hf upload`, and `hf auth login`, which validates via `/api/whoami-v2`) | [V-code] setup.py at v0.34.0 and v1.0.0; [cli/download.py](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/cli/download.py) |

### 2.3 Xet, and whether a plain-HTTP mirror still works

**What Xet is.**
- Xet is the Hub's storage for all models: "all models on the Model Hub are Xet-backed Git repositories" ([models-downloading](https://huggingface.co/docs/hub/models-downloading)) [V-doc].
- It became the default for new users on 2025-05-23 ([changelog](https://huggingface.co/changelog/xet-default-for-new-users)) [V-doc].
- Files are cut into content-defined chunks. The chunks are grouped into xorbs (at most 64 MiB) and described by shards. A download asks the CAS server for a reconstruction and then fetches signed xorb ranges ([Xet docs](https://huggingface.co/docs/xet/index)) [V-doc].

**How the client decides.** It uses Xet for a file only when all of these hold; otherwise it does a plain HTTP GET (`parse_xet_file_data_from_response`, `_download_to_tmp_and_move`) [V-code]:
- the response carries `X-Xet-Hash`;
- the response also carries `Link rel="xet-auth"` or `X-Xet-Refresh-Route`;
- `hf_xet` is installed and not disabled.

**A second signal a mirror must strip.**
- With `hf_xet` installed, `snapshot_download` caches the `/tree` listing. For every entry with a valid `xetHash` it skips the `HEAD` and goes straight to `{ENDPOINT}/api/…/xet-read-token/{commit}` (`_xet_file_metadata_from_tree_cache`; I read it in file_download.py lines 1770–1960) [V-code].
- So a mirror that wants plain HTTP must drop `xetHash` from `/tree` and `/paths-info`, as well as the Xet headers [I, from that code].
- The open-source mirror [olah](https://github.com/vtuber-plan/olah) strips `x-xet-hash` for exactly this reason (`proxy/files.py`) [V-code].

**Upstream still serves Xet files over plain HTTP.** `resolve` redirects to the "Git LFS bridge" on the CDN, which supports `Range` (I saw a 206 live) ([legacy-git-lfs](https://huggingface.co/docs/hub/xet/legacy-git-lfs)) [V-doc, V-live]. So a remote can fetch every file from huggingface.co with plain HTTP.

**Answer to "does a client still work through a plain-HTTP mirror?"**
- **Downloads: yes**, for files up to 50 GB (the client's hard cap), as long as the mirror emits no Xet signal anywhere [V-code + I].
- **Uploads: no.** Since 1.20.0 the client uploads through Xet whenever `hf_xet` is available. The code reads `if is_xet_available(): … _upload_xet_files(…); return` (I read it in `_upload_files`) [V-code]. A local repository without Xet needs `HF_HUB_DISABLE_XET=1` on every uploading client.

**What real Xet support would mean.**
- `xet-read-token` and `xet-write-token` must answer in two forms at once:
  - the headers `X-Xet-Cas-Url`, `X-Xet-Access-Token` and `X-Xet-Token-Expiration`, which the Python side reads;
  - the JSON `{accessToken, exp, casUrl}`, which the Rust `hf_xet` reads.

  In both, `casUrl` must point at Quader.
- The CAS read endpoints: `/v2/reconstructions`, with a v1 fallback.
- For uploads: ingesting xorbs and shards with Xet's hashing. Xet hash strings are byte-reversed per 8-byte block.
- Sources [V-code, V-doc as cited by a sub-researcher]: [xet-core auth_group_builder.rs](https://github.com/huggingface/xet-core/blob/main/xet_pkg/src/xet_session/auth_group_builder.rs), [Xet API](https://huggingface.co/docs/xet/api).
- `hf_xet` itself does not read `HF_ENDPOINT`; it goes wherever `casUrl` says [V-code, from a search in xet-core].

### 2.4 Upload path (for a local repository)

Sources [V-code]: [_commit_api.py](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/_commit_api.py), [lfs.py](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/lfs.py), hf_api.py.

1. **Create the repo:** `POST /api/repos/create` with `{"name","organization","type","visibility"}`, answered with `{"url"}`. A 409 means it exists, which the client accepts under `exist_ok`.
2. **Preupload:** `POST /api/{type}s/{repo}/preupload/{rev}` with `{"files":[{"path","sample"(base64 of the first 512 bytes),"size"}]}`, in batches of 256. The answer gives each file an `uploadMode` of `lfs` or `regular`, plus `shouldIgnore`. The server's rule for `lfs` versus `regular` is not documented [NV].
3. **Content:** either Xet (see 2.3), or the LFS batch call `{"operation":"upload","transfers":["basic","multipart"],"objects":[{"oid":sha256,"size"}],"hash_algo":"sha256"}`. Its answer holds `actions.upload` (a single PUT, or multipart parts plus a completion POST) and an optional `actions.verify`.
4. **Commit:** `POST /api/{type}s/{repo}/commit/{rev}` with `application/x-ndjson`. The lines are:
   - one `header` line;
   - `file` lines (base64 content);
   - `lfsFile` lines (`path`, `algo: sha256`, `oid`, `size`);
   - `deletedFile` and `deletedFolder` lines.

   The answer is `{commitUrl, commitOid}`. `commitUrl` must contain `/commit/`, because the client splits on it.
5. **Branches and tags:** `POST …/branch/{b}` and `POST …/tag/{rev}`. `hf upload` calls `create_repo(exist_ok=True)` first.

### 2.5 Pointing clients at a mirror, auth, gated models, rate limits

**How clients are pointed at a mirror.**

| Client | Setting | Source |
|---|---|---|
| `huggingface_hub` and everything on it (transformers, diffusers, vLLM, SGLang, mlx_lm, TGI) | `HF_ENDPOINT=https://mirror/…` | [V-code] |
| llama.cpp | `MODEL_ENDPOINT`, then `HF_ENDPOINT` | [common.cpp line 1561](https://github.com/ggml-org/llama.cpp/blob/master/common/common.cpp) [V-code] |
| LocalAI | `HF_MIRROR`, then `HF_ENDPOINT` | [pkg/downloader/uri.go](https://github.com/mudler/LocalAI/blob/master/pkg/downloader/uri.go) [V-code] |
| RamaLama | `HF_ENDPOINT` | [huggingface.py](https://github.com/containers/ramalama/blob/main/ramalama/transports/huggingface.py) [V-code] |
| NVIDIA NIM | `HF_ENDPOINT` | [NIM model download](https://docs.nvidia.com/nim/large-language-models/2.1.4/deployment/model-download.html) [V-doc] |

I found no official documentation of running a Hub mirror [NV: a negative cannot be proven]. Public and open-source mirrors exist:
- hf-mirror.com: `HF_ENDPOINT=https://hf-mirror.com`; it recommends `HF_HUB_DISABLE_XET=1` when Xet errors appear ([hf-mirror.com](https://hf-mirror.com/)) [V-doc].
- [olah](https://github.com/vtuber-plan/olah) and [DingoSpeed](https://github.com/dingodb/dingo-hfmirror) [V-doc].

**Auth.**
- The client sends `Authorization: Bearer <token>`, taken from `HF_TOKEN` or the token file ([utils/_headers.py](https://github.com/huggingface/huggingface_hub/blob/main/src/huggingface_hub/utils/_headers.py)) [V-code].
- The configured endpoint counts as a Hub origin, so the client sends the token to the mirror. **A mirror that redirects to huggingface.co makes the client send the user's mirror token to huggingface.co** [V-code + I].
- Quader's catalogue page already warns about the reverse case ([huggingface.md PKG-HF-003](../features/formats/huggingface.md)).

**Gated models.**
- Access is granted per user, never per organisation, and can only be requested in the browser ([models-gated](https://huggingface.co/docs/hub/models-gated)) [V-doc].
- A remote with one upstream token gives every Quader reader whatever that one Hub account has accepted [I].
- The faithful alternative is a per-user Hub token, stored in Quader and sent upstream for gated repos [I].
- Not verified: whether the Hub answers 401 or 403 to an authenticated user without access [NV].

**Rate limits** per 5-minute window ([rate-limits](https://huggingface.co/docs/hub/rate-limits), values "in September '25") [V-doc]. The headers are `RateLimit: "<bucket>";r=…;t=…` and `RateLimit-Policy: "fixed window";"<bucket>";q=…;w=300` [V-live].

| Plan | API | Resolvers (`/resolve/`) | Pages |
|---|---|---|---|
| Anonymous (per IP) | 500 | 3,000 | 100 |
| Free user | 1,000 | 5,000 | 200 |
| PRO | 2,500 | 12,000 | 400 |
| Team | 3,000 | 20,000 | 400 |
| Enterprise | 6,000 | 50,000 | 600 |

- A separate egress allowance answers `X-Error-Code: EgressLimitReached` [V-doc, V-code].
- `huggingface_hub` sleeps for `t` plus 1 s on a 429 [V-code]. A proxy should therefore pass a 429 through together with these headers.

---

## 3. Ollama and LM Studio (question 3)

### 3.1 Ollama: the protocol

| Aspect | What Ollama does | Tag and source |
|---|---|---|
| Base | OCI-Distribution-like HTTP under `{scheme}://{host}/v2/{namespace}/{model}/…`. A maintainer: "we do not have an official registry API… we unofficially use the GET layer, POST/PUT(upload) layer and read/commit manifests". Compatibility with the Docker registry is not guaranteed | [V-issue] [#2388](https://github.com/ollama/ollama/issues/2388) |
| Names | `[scheme://]host/namespace/model[:tag]`, defaults `registry.ollama.ai`, `library`, `latest`. The host may carry a port but **no `/`**. Namespace and model are exactly one segment each, and the namespace allows no `.` | [V-code] `ParseNameBare`, `isValidPart` in [types/model/name.go](https://github.com/ollama/ollama/blob/main/types/model/name.go) (I read it) |
| Manifest | `application/vnd.docker.distribution.manifest.v2+json`. Config `application/vnd.docker.container.image.v1+json` with `model_format`, `model_family`, `file_type`, `capabilities`, `requires` and others. Extra fields `runner` and `format`; per layer `from` and `name` | [V-code] [manifest/manifest.go](https://github.com/ollama/ollama/blob/main/manifest/manifest.go), [types/model/config.go](https://github.com/ollama/ollama/blob/main/types/model/config.go) |
| Manifest list | `application/vnd.ollama.manifest.list.v2+json`. Children are `{mediaType, digest, runner, format}` **without `size`**; runners are `mlx`, `llamacpp` and `ggml`. The client fetches a child as a **blob** | [V-code] manifest.go line 33; `pullSelectedManifest` in [server/images.go](https://github.com/ollama/ollama/blob/main/server/images.go) |
| Layer types | `application/vnd.ollama.image.{model, projector, adapter, embed, template, system, license, params, messages, tensor, json, draft}` | [V-code] layer.go (I read it) |
| Pull | 1. `GET …/manifests/{tag}` with `Accept: application/vnd.ollama.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json` (no OCI type). The digest is computed locally; `Docker-Content-Digest` is not read.<br>2. `HEAD …/blobs/{d}` for `Content-Length`.<br>3. A `GET` whose final answer must carry a `Location`.<br>4. Up to **16 parallel `Range` parts** (part size = size/16, clamped to 100 MB–1,000 MB) against **that one URL, which is reused for every part and every retry**.<br>5. SHA-256 is verified after the download | [V-code] [server/download.go](https://github.com/ollama/ollama/blob/main/server/download.go) lines 100–103 and 219–289 (I read it), `verifyBlob` |
| The `Location` rule | `case http.StatusOK, …StatusTemporaryRedirect, …: loc, err := resp.Location(); if err != nil { return nil, err }`. A blob served directly with `200` and no `Location` fails ("no Location header in response"). A `200` **with** a `Location` header is accepted; hf.co answers its generated blobs that way | [V-code] download.go lines 254–263; [V-issue] [#7244](https://github.com/ollama/ollama/issues/7244) (open: pulls from `registry:2` fail); [V-live, by a sub-researcher] for hf.co |
| Public-address rule | The blob target must resolve to public IP addresses, even when it is the registry's own host. Exceptions: the host is `ollama.com`, `ollama.ai`, `hf.co`, `huggingface.co` or `cloudflarestorage.com` (or a subdomain), an `HTTPS_PROXY` applies, or `--insecure` is given. Refused ranges include RFC 1918, loopback and 100.64/10 | [V-code] `ValidateRedirectTarget`, `NewRedirectClient` ("Redirect destinations do not get the registry host's exemption") in [transfer/redirect.go](https://github.com/ollama/ollama/blob/main/transfer/redirect.go) (I read it). Present in release v0.40.18 [V-code, at the tag]. A regression since 0.35 according to [#18716](https://github.com/ollama/ollama/issues/18716) [V-issue] |
| `--insecure` | Forces `http://` for the registry (`if requestURL.Scheme != "http" && regOpts.Insecure { requestURL.Scheme = "http" }`) and allows private targets. There is no "skip TLS verification" option | [V-code] images.go line 1768 |
| Timeouts | Discovering the `Location` has 30 s. A part with no progress for 30 s is retried without limit ("stalled; retrying") | [V-code] download.go lines 106, 233, 402–423 |
| Push | 1. `HEAD` the blob.<br>2. `POST …/blobs/uploads/` (optionally `?mount=&from=`).<br>3. `PATCH` parts of 100 MB–1 GB with `Content-Range: {start}-{end}` (no `bytes=`) and `X-Redirect-Uploads: 1`.<br>4. `PUT …?digest=…&etag=…`.<br>5. `PUT …/manifests/{tag}`.<br>Tensor models instead start with `POST …/uploads/?digest=…` with no body | [V-code] [server/upload.go](https://github.com/ollama/ollama/blob/main/server/upload.go), [transfer/upload.go](https://github.com/ollama/ollama/blob/main/transfer/upload.go) |
| Auth | 1. The first request carries no credentials.<br>2. On `401` it parses `WWW-Authenticate` (`realm`, `service`, `scope`).<br>3. **The realm host must equal the request host** ("realm host … does not match original host").<br>4. It sends `GET {realm}?service&scope&ts&nonce` with `Authorization: <base64 ed25519 public key>:<base64 signature>` (no scheme word).<br>5. It expects `{"token":…}` and then uses `Bearer`.<br>Basic credentials cannot be set (`username`/`password` are "Deprecated: ignored") | [V-code] [server/auth.go](https://github.com/ollama/ollama/blob/main/server/auth.go) (I read it), images.go `makeRequestWithRetry` lines 1692–1732 (I read it), [api/types.go](https://github.com/ollama/ollama/blob/main/api/types.go) |
| Live registry | registry.ollama.ai: `GET /v2/` answers 404; no tags list and no catalog; the manifest comes without `Docker-Content-Digest`; a blob GET answers `307` to Cloudflare R2 with `X-Amz-Expires=86400`; `chunksums` answers 410 | [V-live, by a sub-researcher] |
| Mirror setting | None. [envconfig/config.go](https://github.com/ollama/ollama/blob/main/envconfig/config.go) has no registry or mirror variable; the host is part of every model name. "Locally-hosted library" ([#914](https://github.com/ollama/ollama/issues/914)) is open | [V-code], [V-issue] |
| Storage | `$OLLAMA_MODELS` (default `~/.ollama/models`): `blobs/sha256-<hex>`, manifests under `manifests-v2/{host}/{ns}/{model}/{tag}` (older layout: `manifests/…`) | [V-code] [manifest/paths.go](https://github.com/ollama/ollama/blob/main/manifest/paths.go); [FAQ](https://github.com/ollama/ollama/blob/main/docs/faq.mdx) [V-doc] |
| `hf.co/{user}/{repo}:{quant}` | Ollama pulls `https://hf.co/v2/{user}/{repo}/manifests/{quant}`. hf.co builds a Docker v2 manifest on the fly with `…image.model` (the GGUF; **its digest is the file's LFS SHA-256**), `…template` and `…params`. The model blob answers `307 Location: /{repo}/resolve/main/{file}.gguf`. The default quant is `Q4_K_M`. The template is chosen from `tokenizer.chat_template`, or overridden by repo files `template`, `system`, `params`. Private repos work through the user's Ollama SSH key registered at HF | [V-live] my own requests: the manifest of `bartowski/Llama-3.2-1B-Instruct-GGUF:Q4_K_M`, the blob `307`, and the digest `6f85a640…` equal to the tree's `lfs.oid`. [HF and Ollama](https://huggingface.co/docs/hub/ollama) [V-doc] |
| hf.co private repos today | Broken through `hf.co/…`: hf.co answers `Basic realm="https://huggingface.co/api/models/{repo}/ssh-auth"`, whose host differs from the request host | [V-issue] [#15661](https://github.com/ollama/ollama/issues/15661) open |
| Create | `ollama create` from GGUF (GGUF is not quantised on import) or from a safetensors directory (MLX tensor layers; `-q` quantises) | [V-doc] [docs/import.mdx](https://github.com/ollama/ollama/blob/main/docs/import.mdx); [V-code] [create/quantize.go](https://github.com/ollama/ollama/blob/main/create/quantize.go) |

**Can a plain OCI registry hold and serve Ollama models as is?**
- **Hold: yes**, for the Docker v2 manifests with arbitrary layer types. `registry:2` accepts the pushes ([#7244](https://github.com/ollama/ollama/issues/7244), [#914 comment](https://github.com/ollama/ollama/issues/914#issuecomment-1953482174)) [V-issue].
- **Serve to Ollama: no**, unless the registry does all of the following [V-code + V-issue + I]:
  - answers blob GETs with a cross-origin redirect, or with a `Location` header, to a public address;
  - puts its token realm on the same host;
  - accepts Ollama's signed token request, or serves anonymously;
  - accepts the Ollama manifest-list type, which newer models use.
- Harbor fails the handshake: its `Basic realm="harbor"` is read as a URL ([#4204](https://github.com/ollama/ollama/issues/4204)) [V-issue].

### 3.2 LM Studio: what it needs

| Aspect | Finding | Tag and source |
|---|---|---|
| Source | Hugging Face: "a built-in model downloader that let's you download any supported model from Hugging Face" | [V-doc] [download-model](https://lmstudio.ai/docs/app/basics/download-model) |
| Endpoints | Logs show `https://huggingface.co/api/models/{user}/{repo}/tree/main?recursive=true&expand=false` and `resolve/main/{file}`. With the vendor's optional HF proxy: `https://search.lmstudio.ai/v1/hf-proxy/{user}/{repo}/resolve/main/{file}`, a vendor service that cannot be self-hosted | [V-issue] [bug-tracker #845](https://github.com/lmstudio-ai/lmstudio-bug-tracker/issues/845), [#2634](https://github.com/lmstudio-ai/lmstudio-bug-tracker/issues/2634), [#256](https://github.com/lmstudio-ai/lmstudio-bug-tracker/issues/256#issuecomment-2620673643) |
| Download code | In the closed-source app. `lms get` only plans downloads through the app's SDK, and accepts LM Studio Hub artifacts, staff picks or `https://huggingface.co/…` URLs ("Only https://huggingface.co URLs are supported") | [V-code] [lms get.ts](https://github.com/lmstudio-ai/lms/blob/main/src/subcommands/get.ts); app code [NV: closed] |
| Formats | GGUF (llama.cpp engine); MLX on Apple Silicon (mlx-engine, built on mlx-lm and mlx-vlm) | [V-doc] [mlx-engine](https://github.com/lmstudio-ai/mlx-engine) |
| `HF_ENDPOINT` | Ignored. "HF_ENDPOINT is not used when downloading modes." has been open since 2025-03. In lms, "How to configure the download url prefix of https://huggingface.co" has been open since 2024-11 | [V-issue] [#474](https://github.com/lmstudio-ai/lmstudio-bug-tracker/issues/474), [lms #104](https://github.com/lmstudio-ai/lms/issues/104) (I checked both: open) |
| Proxy | 2024: "we currently do not support setting proxy server yet" ([#1](https://github.com/lmstudio-ai/lmstudio-bug-tracker/issues/1)). Users report that `HTTP(S)_PROXY`, `NODE_EXTRA_CA_CERTS` and `NODE_USE_SYSTEM_CA=1` work ([#1588](https://github.com/lmstudio-ai/lmstudio-bug-tracker/issues/1588)); this is not documented | [V-issue] |
| Storage | `~/.lmstudio/models/{publisher}/{repo}/{file}`. The home directory is resolved via `~/.lmstudio-home-pointer`, `~/.cache/lm-studio` or `~/.lmstudio`. The folder can be changed in "My Models" | [V-doc] [import-model](https://lmstudio.ai/docs/app/advanced/import-model); [V-code] [findLMStudioHome](https://github.com/lmstudio-ai/lmstudio-js/blob/main/packages/lms-common-server/src/findLMStudioHome.ts) |
| Import | `lms import <file>` with `--user-repo`, `--copy`, `--hard-link`, `--symbolic-link`; by default it moves the file. Import from a URL is not documented | [V-doc] [cli import](https://lmstudio.ai/docs/cli/local-models/import) |
| LM Studio Hub | `model.yaml` holds metadata only (`sources: [{type: huggingface, user, repo}]`); `lms clone` and `lms push` move it. The weights stay on HF | [V-doc] [modelyaml](https://lmstudio.ai/docs/app/modelyaml), [publish](https://lmstudio.ai/docs/app/modelyaml/publish) |
| Offline | Chat, RAG and the local server work offline; search and downloads need the network | [V-doc] [offline](https://lmstudio.ai/docs/app/offline) |

**What LM Studio needs from Quader:** nothing it can use directly. There are only two paths:
- **(a) Sideloading:** files from a generic repository, imported with `lms import`.
- **(b) [I] A forward proxy the organisation already runs** that intercepts TLS and routes `huggingface.co` to a Quader Hugging Face remote served at a host root. The proxy's CA must be trusted by the app's Node runtime. This is outside Quader and unsupported by the vendor.

---

## 4. Further formats and tools (question 4)

| Tool or hub | Weight format | Protocol | Private mirror? | Relevance for local LLMs | Source |
|---|---|---|---|---|---|
| **llama.cpp `-hf user/repo[:quant]`** | GGUF (+ `mmproj`, split files) | Hub API: `GET {ep}api/models/{repo}/refs` for `main`'s `targetCommit`; `GET {ep}api/models/{repo}/tree/{commit}?recursive=true` (**first page only**); `{ep}{repo}/resolve/{commit}/{path}` with `HEAD` and `Range: bytes=n-` resume. UA `llama-cpp/{build}`. The HF cache layout is shared with `huggingface_hub`. No hash check. Before 2026-03-24 (PR #20775) it used `{ep}v2/{repo}/manifests/{tag}` | yes: `MODEL_ENDPOINT`, then `HF_ENDPOINT` | high | [V-code] [common/hf-cache.cpp](https://github.com/ggml-org/llama.cpp/blob/master/common/hf-cache.cpp) lines 189, 219, 302, 340 (I read them); [common.cpp](https://github.com/ggml-org/llama.cpp/blob/master/common/common.cpp); [download.cpp](https://github.com/ggml-org/llama.cpp/blob/master/common/download.cpp) |
| llama.cpp `-mu URL` / `-dr repo` | GGUF | any HTTP URL / Docker Hub only (`auth.docker.io` and `registry-1.docker.io` hard-coded; takes the first `gguf` layer) | `-mu` yes, `-dr` no | medium / low | [V-code] download.cpp `common_docker_resolve_model` |
| **vLLM** | safetensors (GGUF "highly experimental", via a plugin) | Hub via `huggingface_hub`; ModelScope (`VLLM_USE_MODELSCOPE`); S3, GCS, Azure via `runai_streamer`; tensorizer | `HF_ENDPOINT`; `MODELSCOPE_ENDPOINT`; S3 endpoint variables | high for GPU servers | [V-doc] [GGUF](https://docs.vllm.ai/en/latest/features/quantization/gguf.html), [env vars](https://docs.vllm.ai/en/latest/configuration/env_vars.html), [Run:ai streamer](https://docs.vllm.ai/en/latest/models/extensions/runai_model_streamer.html) |
| SGLang | safetensors | Hub; ModelScope (`SGLANG_USE_MODELSCOPE`) | `HF_ENDPOINT` [I] | medium | [V-doc] [env vars](https://docs.sglang.io/references/environment_variables.html) |
| Text Generation Inference | safetensors (GPTQ, AWQ, …) | Hub | `HF_ENDPOINT` [I] | low: archived 2026-03-21, maintenance mode | [V-doc] [repo](https://github.com/huggingface/text-generation-inference) |
| **Docker Model Runner** | GGUF, safetensors, MLX, DDUF | OCI in two packagings. Docker format: config `application/vnd.docker.ai.model.config.v0.1+json` or `v0.2`; layers `application/vnd.docker.ai.gguf.v3`, `.safetensors`, `.vllm.config.tar`, `.dir.tar`, `.license`, `.mmproj`, `.chat.template.jinja`, `.model.file`. Or ModelPack (`--format cncf`). `hf.co/…` pulls use the Hub REST API directly | OCI: any registry, plus registry mirrors (`MODEL_RUNNER_REGISTRY_MIRRORS` or the daemon's `registry-mirrors`) tried before Docker Hub. HF: base URL fixed, no `HF_ENDPOINT` | high (Docker Desktop users; Docker Hub `ai/`) | [V-code] [types/config.go](https://github.com/docker/model-runner/blob/main/pkg/distribution/types/config.go) (I read it), [huggingface/client.go](https://github.com/docker/model-runner/blob/main/pkg/distribution/huggingface/client.go), [remote.go](https://github.com/docker/model-runner/blob/main/pkg/distribution/oci/remote/remote.go); [V-doc] [docker model package](https://docs.docker.com/reference/cli/docker/model/package/); [V-live] `ai/smollm2:latest` (my own request) |
| **CNCF ModelPack** | any, one raw layer per file | OCI 1.1: `artifactType: application/vnd.cncf.model.manifest.v1+json`; config `application/vnd.cncf.model.config.v1+json`; layers `application/vnd.cncf.model.{weight,weight.config,doc,code,dataset}.v1.{raw,tar,tar+gzip,tar+zstd}`; annotations `org.cncf.model.filepath`, `org.cncf.model.file.metadata+json`, `org.cncf.model.file.mediatype.untested`. Needs no `subject` or referrers | any OCI 1.1 registry | medium-high. Docker Hub `ai/…-safetensors` and `ai/…-mlx` already ship it; also used by Kubernetes image volumes, the model CSI driver, Harbor, Dragonfly, RamaLama, KitOps | [V-code] [mediatype.go](https://github.com/modelpack/model-spec/blob/main/specs-go/v1/mediatype.go) (I read it); [V-doc] [spec.md](https://github.com/modelpack/model-spec/blob/main/docs/spec.md), [annotations.md](https://github.com/modelpack/model-spec/blob/main/docs/annotations.md); [V-live] `ai/smollm2:360m-safetensors` (my own request) |
| KitOps ModelKit | any (tar or raw layers) | OCI: `artifactType` `application/vnd.kitops.modelkit.manifest.v1+json` (an index: `…index.v1+json`); config `application/vnd.kitops.modelkit.config.v1+json`; layers `application/vnd.kitops.modelkit.{model,modelpart,dataset,code,docs}.v1.{tar,raw}[+gzip\|+zstd]`. `kit pack --use-model-pack` writes ModelPack; `kit import` pulls from HF | any OCI registry | medium (MLOps) | [V-code] [kitops.go](https://github.com/kitops-ml/kitops/blob/main/pkg/lib/constants/mediatype/kitops.go), [artifacttype.go](https://github.com/kitops-ml/kitops/blob/main/pkg/lib/constants/mediatype/artifacttype.go). The [spec page](https://kitops.org/docs/modelkit/spec/) still shows older `vnd.jozu.*` names [V-doc, disagrees with the code] |
| ORAS artifacts | any | OCI 1.1. Default `artifactType` is `application/vnd.unknown.artifact.v1`; file layers are `application/vnd.oci.image.layer.v1.tar`, with names in `org.opencontainers.image.title` | any OCI registry | low (no model semantics) | [V-code] [oras push.go](https://github.com/oras-project/oras/blob/main/cmd/oras/root/push.go), [oras-go file.go](https://github.com/oras-project/oras-go/blob/main/content/file/file.go) |
| RamaLama | GGUF, safetensors | transports `hf://`, `ollama://`, `oci://`, `modelscope://`, `https://`. Pushes `car` (an image with the model under `/models`) or `raw` images | `HF_ENDPOINT`; any OCI registry | medium (Podman users) | [V-doc] [ramalama.1.md](https://github.com/containers/ramalama/blob/main/docs/ramalama.1.md), [ramalama-push.1.md](https://github.com/containers/ramalama/blob/main/docs/ramalama-push.1.md) |
| LocalAI | GGUF and others | `huggingface://` / `hf://`; `oci://` (any registry, with cosign); `ollama://` (fixed to registry.ollama.ai); `http(s)://`; galleries (a YAML index, configurable with mirrors) | HF: `HF_MIRROR` / `HF_ENDPOINT`. OCI: yes. Ollama: no | medium | [V-code] [uri.go](https://github.com/mudler/LocalAI/blob/master/pkg/downloader/uri.go), [pkg/oci/ollama.go](https://github.com/mudler/LocalAI/blob/master/pkg/oci/ollama.go), [core/cli/run.go](https://github.com/mudler/LocalAI/blob/master/core/cli/run.go) |
| Jan | GGUF, MLX | Hub API (`/api/models/{repo}?blobs=true…`, `resolve/main`), hard-coded; requests go first through Jan's own mirror `apps.jan.ai/huggingface.co/…` | no `HF_ENDPOINT`; an app proxy setting with `ignore_ssl` | medium | [V-code] [default.ts](https://github.com/janhq/jan/blob/main/web-app/src/services/models/default.ts), [helpers.rs](https://github.com/janhq/jan/blob/main/src-tauri/src/core/downloads/helpers.rs) |
| GPT4All | GGUF | A hard-coded list `gpt4all.io/models/models3.json` with per-model URLs (mostly HF `resolve`) and an MD5 check. **TLS peer verification is off** (`VerifyNone`). The repo was last pushed 2025-05 | no | low | [V-code] [modellist.cpp](https://github.com/nomic-ai/gpt4all/blob/main/gpt4all-chat/src/modellist.cpp), [download.cpp](https://github.com/nomic-ai/gpt4all/blob/main/gpt4all-chat/src/download.cpp) |
| KoboldCpp | GGUF, safetensors | any `http(s)` URL ending in `.gguf`, `.safetensors` or similar; downloads with aria2c over 16 connections with `--continue` | yes: any URL | low-medium | [V-code] [koboldcpp.py](https://github.com/LostRuins/koboldcpp/blob/concedo/koboldcpp.py) |
| llamafile | executable with an embedded GGUF | plain file download | any URL | low | [V-doc] see section 1 |
| mlx_lm | MLX | `huggingface_hub` `snapshot_download`; ModelScope with `MLXLM_USE_MODELSCOPE` | `HF_ENDPOINT` | high on Macs | [V-code] mlx_lm utils.py |
| **ModelScope** | safetensors, GGUF (mirrors of HF models) | Own API: `/api/v1/{type}s/{repo}/repo/files`, `/repo?Revision=&FilePath=`, `/revisions`, LFS batch, commit. Responses are wrapped in `{"Data":…}`. Auth is a cookie `m_session_id` plus Bearer | `MODELSCOPE_ENDPOINT` | medium (China; vLLM, SGLang, mlx_lm, NIM and KServe can use it) | [V-code] [modelscope_hub _legacy_api.py](https://github.com/modelscope/modelscope_hub/blob/main/src/modelscope_hub/_legacy_api.py) |
| NVIDIA NGC / NIM | `.nemo`, TensorRT-LLM engines, HF layout [NV for the formats] | NGC API and CLI. NIM sources: `ngc://`, `hf://`, `s3://`, `gs://`, `modelscope://` | NIM: `HF_ENDPOINT`, `MODELSCOPE_ENDPOINT`, `NIM_REPOSITORY_OVERRIDE` (`s3://`, `gs://`, `http(s)://`). NGC CLI: none documented | low for local use, medium for enterprise GPU | [V-doc] [NIM model download](https://docs.nvidia.com/nim/large-language-models/2.1.4/deployment/model-download.html), [air gap](https://docs.nvidia.com/nim/large-language-models/2.1.4/deployment/air-gap-deployment.html), [NGC CLI](https://docs.ngc.nvidia.com/cli/cmd_registry.html) |
| Kaggle Models | per framework (GGUF, Transformers, Keras, …) | RPC-style `api.kaggle.com`. `KAGGLE_API_ENVIRONMENT` only picks among fixed hosts | no | low (Gemma) | [V-code] [kaggle_env.py](https://github.com/Kaggle/kaggle-sdk-python/blob/main/kagglesdk/kaggle_env.py) |
| PyTorch Hub / TF Hub | pickle / SavedModel | PyTorch Hub: GitHub zipball hard-coded, weights by URL. TF Hub: tfhub.dev redirects to Kaggle; any `http(s)` handle with `?tf-hub-format=compressed` works | weights by URL only | very low | [V-code] [torch/hub.py](https://github.com/pytorch/pytorch/blob/main/torch/hub.py); [V-doc] [tensorflow/hub](https://github.com/tensorflow/hub) |
| MLflow, Triton, KServe | various | S3, GCS, Azure and HTTP stores. KServe also takes `hf://` and `oci://` modelcars (a plain image with the model under `/models`) | via object stores; KServe modelcars via any OCI registry | low (server-side) | [V-doc] [MLflow artifact store](https://mlflow.org/docs/latest/self-hosting/architecture/artifact-store/), [Triton repository](https://github.com/triton-inference-server/server/blob/main/docs/user_guide/model_repository.md), [KServe OCI](https://kserve.github.io/website/docs/model-serving/storage/providers/oci) |
| Foundry Local | ONNX | closed catalogue | not documented | low | [V-doc] [architecture](https://learn.microsoft.com/en-us/azure/ai-foundry/foundry-local/concepts/foundry-local-architecture) |

---

## 5. Overall table

Key to the last column:
- `HF` = a Hugging Face format.
- `Ollama` = an Ollama format.
- `OCI` = the existing OCI format.
- `generic` = the existing generic format. Its local class is built; its remote class is not ([generic.md](../spec/formats/generic.md)).

| Tool | File formats | Protocol | Private mirror possible? How | What Quader would implement |
|---|---|---|---|---|
| `huggingface_hub`, `hf` CLI, transformers, diffusers | safetensors, GGUF, any file | Hub API, Xet | yes: `HF_ENDPOINT`, `HF_TOKEN`; `HF_HUB_DISABLE_XET=1` for LFS uploads | **HF** (read; uploads for local) |
| vLLM, SGLang, TGI | safetensors (+ quantised) | Hub API via `huggingface_hub`; ModelScope; S3 | yes: `HF_ENDPOINT`; `MODELSCOPE_ENDPOINT` | **HF**; ModelScope only on demand |
| mlx_lm | MLX | Hub API | yes: `HF_ENDPOINT` | **HF** |
| llama.cpp | GGUF | Hub API (`refs`, `tree`, `resolve`); plain URL; Docker Hub | yes: `MODEL_ENDPOINT`/`HF_ENDPOINT`; `-mu` takes any URL; `-dr` no | **HF** (the tree's first page must hold the file list); generic for `-mu` |
| **Ollama** | GGUF (+ safetensors/MLX tensor layers) | Ollama registry (OCI-like, section 3.1); the hf.co Ollama view | only by writing the mirror's host into every model name (`mirror/library/llama3.2:3b`); there is no setting | **Ollama** (own format on OCI internals, host-root binding, Ollama auth) + the Ollama view on **HF** |
| LM Studio | GGUF, MLX | Hub API, hard-coded; LM Studio Hub (metadata) | no (`HF_ENDPOINT` is ignored); sideload with `lms import` | nothing on the protocol side; generic for files to sideload |
| Jan | GGUF, MLX | Hub API, hard-coded; own mirror | no; app proxy setting | nothing on the protocol side |
| GPT4All | GGUF | own list + URLs | no | nothing |
| KoboldCpp, llamafile | GGUF | plain HTTP URL | yes: the URL | generic (exists) |
| LocalAI | GGUF, others | HF, OCI, Ollama (fixed), galleries | HF and OCI yes; Ollama no | **HF**; OCI (exists) |
| RamaLama | GGUF, safetensors | HF, OCI, Ollama, ModelScope | HF (`HF_ENDPOINT`) and OCI yes | **HF**; OCI (exists) |
| Docker Model Runner | GGUF, safetensors, MLX | OCI (Docker format, ModelPack); Hub directly | OCI yes (any registry, registry mirrors); HF no | OCI (exists; size limits) |
| modctl, KitOps, ORAS | any | OCI 1.1 artifacts | yes: any registry | OCI (exists; size limits) |
| KServe modelcars, Kubernetes image volumes | any | OCI images or artifacts | yes | OCI (exists) |
| NVIDIA NIM | HF layout, NGC formats | NGC, HF, ModelScope, S3, HTTP | `HF_ENDPOINT`, `NIM_REPOSITORY_OVERRIDE` | **HF** |
| ModelScope SDK | safetensors, GGUF | ModelScope API | `MODELSCOPE_ENDPOINT` | ModelScope (only on demand) |
| Kaggle, PyTorch Hub, TF Hub, MLflow, Triton | various | own APIs or object stores | no, or object-store only | nothing |

---

## 6. Per protocol: remote, local, hard parts

### 6.1 Hugging Face Hub

**Remote (caching proxy of huggingface.co).** All points are [I] from section 2 unless tagged otherwise.

- **Endpoints:**
  - `GET /api/models/{repo}` and `/revision/{rev}`; keep `sha` and `siblings` for 0.x clients.
  - `/tree/{rev}[/{path}]` with `recursive`, `expand` and cursor paging, the `Link` header rewritten to Quader's host. llama.cpp reads only the first page.
  - `POST /paths-info/{rev}` (form-encoded).
  - `GET /refs`.
  - `HEAD` and `GET /{repo}/resolve/{rev}/{path}`.
  - `GET /api/whoami-v2`.
  - Datasets only if wanted.
- **Headers on every `resolve` answer:**
  - `X-Repo-Commit` (40 hex);
  - `X-Linked-Etag`/`ETag`: SHA-256 for LFS files, git SHA-1 otherwise. Take it from upstream's `X-Linked-Etag`, never from the CDN's `ETag`;
  - `X-Linked-Size` and `Accept-Ranges: bytes`;
  - real `206` answers to ranges;
  - `X-Error-Code` on errors, including a 404 `EntryNotFound` that still carries `X-Repo-Commit`.
- **No Xet signals at all**, neither headers nor `xetHash` in listings, until Quader serves Xet itself. Until then, `huggingface_hub` cannot fetch files over 50 GB [V-code]; everything else works.
- **Uncached files:** answer the `HEAD` from upstream's `HEAD` without filling the cache. The client's metadata timeout defaults to 10 s; Quader's catalogue page sets `HF_HUB_ETAG_TIMEOUT=86400` for this reason ([PKG-HF-008](../features/formats/huggingface.md)).
- **Revisions:** resolve `rev → commit` with a metadata period. Everything addressed by commit is immutable and can be cached for ever. Branches other than `main` matter, because EXL2 bitrates live on branches [V-live].
- **Upstream identity:** anonymous access gets 500 API and 3,000 resolver calls per 5 minutes per IP [V-doc]. For real use the remote needs:
  - a Hub token of a paid plan;
  - metadata caching;
  - 429 pass-through with the `RateLimit` headers.
- **Gated models:** choose between one upstream token (every reader inherits that account's licence acceptances) and per-user Hub tokens.

**Local (push and keep).**
- The read path above, served from Quader's own revisions. Quader mints 40-hex commit ids and per-file ETags: SHA-256 for large files, and the git blob SHA-1 `sha1("blob <len>\0"+content)` for small ones [I; git object hashing is standard git, not quoted from these sources].
- The upload API of 2.4, plus one content channel [V-code for the client side]:
  - **either** the Xet write path (CAS xorbs, shards, write tokens),
  - **or** the LFS batch API (basic, multipart), with every uploading client required to set `HF_HUB_DISABLE_XET=1`.
- `git push` and `git clone` are optional extras with a large surface.

**Hard parts:**
- files of tens of GB, with Range and resume on both sides;
- Xet: two signals to strip for reads, and a whole CAS protocol for writes;
- the 50 GB plain-HTTP cap;
- two ETag kinds;
- branch-to-commit consistency between model info and `resolve`. Quader's own catalogue notes: "return the same commit hash from the model information and from `X-Repo-Commit`";
- gated models;
- the upstream's small API budget.

### 6.2 Ollama registry

**Remote (caching proxy of registry.ollama.ai).** All points are [I] from section 3.1 unless tagged otherwise.

- **Addressing:** serve it on a **host bound to the repository**, because Ollama names allow exactly `host/namespace/model`. Users write `ollama pull ollama.mirror.example.com/library/llama3.2:3b`.
- **Manifests:**
  - Fetch them with Ollama's own `Accept`, including the list type, and store the bytes verbatim (the client hashes them).
  - Refresh tags after a metadata period. Upstream has no tags list, so this can only happen on demand.
  - Follow the blob `307` to R2.
- **`HEAD …/blobs/{d}`:** answer with `Content-Length`.
- **Every blob `GET`:** answer in one of two ways, then serve `206` answers to up to 16 parallel ranges:
  - a `307` to a signed object-store link whose host resolves to a public address and which **stays valid for the whole download**, because Ollama uses one URL for all parts and retries;
  - or a `200` with a `Location` header naming the blob URL itself (what hf.co does for its generated blobs).
- **Cold cache:**
  - The discovery `GET` must still be answered within 30 s.
  - Ranges of a blob whose fill is still running must be served, or the client loops on 30 s stall retries until the fill ends [V-code for the client timeouts; I for the consequence].
- **List type:** accept `application/vnd.ollama.manifest.list.v2+json`, with children that have no `size`, and serve the children as blobs too.
- **Connection test:** upstream has no `/v2/` ping (it answers 404), so a test must probe a manifest instead.

**Local (push).**
- Ollama's token handshake, on the same host [V-code for the client; I for the server side]:
  - verify an ed25519 signature over `GET,{token URL},{base64(hex(sha256("")))}` against public keys the user has registered (as ollama.com and Hugging Face do);
  - answer `{"token":…}`.

  Without it, Ollama can neither push nor pull models that are not public. Basic credentials cannot be configured in Ollama at all.
- The upload flow of 3.1 (`PATCH` with `Content-Range: a-b`, then `PUT ?digest=&etag=`). The tensor-model push (`POST ?digest=` with no body) still has to be checked against Quader's monolithic-upload rule [NV].

**Client-side blocker outside Quader's control.**
- Since Ollama 0.35, every blob target must resolve to a public address, unless the client has `HTTPS_PROXY` set for it or uses `--insecure` [V-code, V-issue].
- So on a private network over HTTPS, Ollama clients need a proxy or `--insecure`. `--insecure` forces plain `http://` for the registry, which conflicts with TLS everywhere [V-code + I].
- The rule has moved several times in the last two months ([#18716](https://github.com/ollama/ollama/issues/18716), [#19345](https://github.com/ollama/ollama/issues/19345), [#19841](https://github.com/ollama/ollama/issues/19841)) [V-issue].

**Hard parts:**
- the redirect and public-address rules;
- link lifetime against 16 ranges of up to 1 GB each;
- the signed-key auth;
- the list type;
- mutable tags without a tags list;
- an upstream format that is still changing: the `transfer` package calls the tensor storage format "under development" ([transfer.go](https://github.com/ollama/ollama/blob/main/transfer/transfer.go)) [V-code].

**The Ollama view on the HF format** (`/v2/{user}/{repo}/manifests/{quant}`, built on the fly from GGUF files):
- The model layer's digest is the GGUF's LFS SHA-256 [V-live]. So the blob the HF remote stores serves both views without a copy [I].
- Template and params blobs are generated. hf.co reuses Ollama's template blobs: the digest `d502d55c…` equals registry.ollama.ai's [V-live, by a sub-researcher]. Quader would have to build them from `tokenizer.chat_template` or pass hf.co's through.

### 6.3 OCI model artifacts (Docker Model Runner, ModelPack, KitOps, ORAS, modelcars)

**Remote and local:** the existing OCI format, with nothing format-specific beyond what section 7 lists.
- Docker Hub's `ai/` namespace comes through the existing Docker Hub remote (`docker model pull quader.example.com/web/docker-hub/ai/smollm2`) and through a bound mirror host for `MODEL_RUNNER_REGISTRY_MIRRORS` [I; not tested].
- None of the producers I read needs referrers or `subject` [V-code, for the manifests and code read]; Quader has them anyway.

**Hard parts:**
- Blob size and the spool (section 7). One 65 GB layer per file is normal [V-live].
- Chunked uploads through any edge proxy must not buffer the body. distribution-spec has no max-chunk hint ([#485](https://github.com/opencontainers/distribution-spec/issues/485)) [V-issue].

### 6.4 Plain HTTP files

This covers `llama.cpp -mu`, KoboldCpp, llamafile, NIM's `NIM_REPOSITORY_OVERRIDE=https://…`, and the files that LM Studio, Jan and GPT4All users sideload.
- The generic format's local class already serves them with `Range` and `ETag` ([R-STOR-012](../spec/storage.md)) [V-spec].
- A generic remote is not built ([generic.md](../spec/formats/generic.md) status) [V-spec].

### 6.5 ModelScope

ModelScope has its own API (section 4), so it would be a separate format if it is ever built [V-code]:
- **Remote:** the file-list and download endpoints, with the `Data` envelope and the cookie-plus-Bearer auth.
- **Local:** commit and LFS batch.

It is only worth building for users in China, or for a vLLM or NIM setup that prefers ModelScope [I].

### 6.6 Cross-cutting hard parts

- **Integrity.**
  - SHA-256 is the common key. HF's LFS `oid`, Ollama's layer digest for the same GGUF and an OCI layer digest are the same value [V-live, for one GGUF].
  - Quader's blob store is content-addressed ([R-STOR-001](../spec/storage.md)) [V-spec], so one GGUF reached through HF, Ollama and OCI is stored once [I].
  - Exceptions: HF's small files use git SHA-1, and Xet uses its own Merkle hash.
  - Clients verify differently: Ollama verifies SHA-256; `huggingface_hub` and llama.cpp verify only the size; GPT4All checks MD5 [V-code].
- **Revisions.**
  - HF: a branch resolves to a commit, and commits are immutable. EXL2 bitrates live on branches.
  - Ollama: tags are mutable, and there is no tags list.
  - OCI: tags are mutable, digests immutable.
- **Range and resume.** Every client uses it [V-code]:
  - Ollama: 16 parallel ranges against one URL.
  - `huggingface_hub` and llama.cpp: `bytes=n-` resume, with a mandatory `206`.
  - KoboldCpp: 16 aria2c connections.
- **Licences and gated models.** HF needs per-user acceptance upstream. Ollama and Docker `ai/` carry `.license` layers [V-doc, V-code].
- **Malware scanning.**
  - Quader hands parts above `malware.scanner.max_file_size` (1 GiB) to the splitter. A part that cannot be split (no tar, no compression) is `unscannable` `limits_exceeded` ([malware-scanning.md](../spec/malware-scanning.md) line 587) [V-spec].
  - So every GGUF and safetensors shard above 1 GiB would be `unscannable` [I].
  - The meaningful check for model files is a pickle-import scan of `.bin`/`.pt`/`.pkl` files, as the Hub does ([security-pickle](https://huggingface.co/docs/hub/security-pickle)) [V-doc].

---

## 7. What Quader's existing OCI format already covers

**What I checked:**
- [docs/spec/formats/oci.md](../spec/formats/oci.md): R-OCI-001, R-OCI-003, R-OCI-004, R-OCI-009, R-OCI-010, R-OCI-011, R-OCI-013, R-OCI-020, R-OCI-022, R-OCI-023, R-OCI-029.
- [endpoints.md](../spec/endpoints.md): R-EP-007.
- [storage.md](../spec/storage.md): R-STOR-004, R-STOR-012, R-STOR-014 and the upload-session settings.
- [identity-and-authentication.md](../spec/identity-and-authentication.md): R-AUTH-001.
- [malware-scanning.md](../spec/malware-scanning.md): the limits.

**What I could not check:**
- The code's actual behaviour; I ran nothing against Quader.
- How a remote fill treats an upstream manifest of an unknown media type.
- Whether the maximum upload size bounds a remote fill. The spec says fills go through the same pipeline, so I infer that it does.

| Need | What Quader's spec says | Verdict |
|---|---|---|
| OCI manifest with a model config type (Docker Model Runner's `application/vnd.docker.ai.model.config.v0.1+json`) | "Config and layer media types and `artifactType` are not restricted beyond that form: the registry stores any OCI content (PKG-OCI-008)" (R-OCI-013 step 3, oci.md line 624) [V-spec] | **covered** |
| `artifactType` (ModelPack, KitOps) stored and returned | stored byte for byte; an `artifact_type` column; the referrers filter uses it (R-OCI-007, R-OCI-016) [V-spec] | **covered** |
| Non-standard fields (KitOps `modelMetadata`, Ollama `from`) | "unknown fields are accepted and ignored (the bytes are stored as they came)" (line 628) [V-spec] | **covered** |
| Docker v2 schema 2 manifests with Ollama layer types | an accepted media type, with arbitrary layer types (R-OCI-013 steps 2–3) [V-spec] | **covered for holding** |
| Ollama manifest list `application/vnd.ollama.manifest.list.v2+json`, children without `size` | only the four OCI/Docker manifest types; "every other type answer[s] 400 `MANIFEST_INVALID`"; every descriptor needs "a non-negative integer size" (lines 617–624) [V-spec] | **refused** |
| Ollama's names `host/namespace/model:tag` | Quader's address is `/v2/{project}/{key}/{name}` (R-OCI-001), which Ollama's grammar cannot express. A host bound to one repository serves `/v2/<name>` (R-EP-007, R-OCI-029) [V-spec] | **only through a bound host per repository** |
| Ollama's auth | The token service takes `Basic` or no credential (R-OCI-004). By R-AUTH-001, an `Authorization` header that is neither `Bearer` nor `Basic` is a malformed credential: 401, without falling through to anonymous [V-spec; I that Ollama's header lands there] | **not covered.** Anonymous pulls of a public repository may work, because Ollama's first request carries no credential [I, untested] |
| Ollama's blob `GET` with a `Location` to a public address | Signed links only when all of these hold: the installation's switch is on (off by default), the file is at least `minSize` (1 MiB), the connection is not encrypted, no Bastion is between, and the request is not a range (R-STOR-014, R-STOR-070). Link life: default 60 s, at most 3,600 s [V-spec] | **partly.** Only with the switch on and a public object-store host. For models above about 16 GB (more than 16 parts of 1 GB), later parts start after the link has expired and fail, because Ollama reuses one URL [I, from the code read]. Never through a Bastion |
| Docker Hub `ai/` as a remote | ordinary OCI content through the Docker Hub preset [V-spec] | **covered** [I, untested] |
| Large blobs | `storage.upload.max_size` is 10 GiB (default, proposed). `storage.spool.max_bytes` is 50 GiB per replica, and `max_size` must not exceed it. `part_size` 16 MiB with at most 10,000 parts gives about 156 GiB. Fills are "committed through the upload pipeline" (R-STOR-004; repositories-and-proxy.md line 2956) [V-spec] | **not by default.** A 65 GB GGUF layer is refused at push and, by inference, at fill. A model repository needs `maxUploadSize` well above 10 GiB, a larger part size beyond about 156 GiB, and spool disk per replica of at least the largest file times the concurrent transfers |
| Range and resume | `206` (R-OCI-010, R-STOR-012); every blob body carries `Content-Length` [V-spec] | **covered** |
| Chunked upload as Ollama sends it (`Content-Range: a-b`) | "`<start>-<end>` in decimal" (R-OCI-011) [V-spec] | **covered** [I] |
| Malware scan of weights | unsplittable parts above 1 GiB are `unscannable` [V-spec] | every large weight file would be `unscannable` [I] |

**Conclusion [I].** Quader's OCI format already is a model registry for ModelPack, Docker Model Runner, KitOps and ORAS, apart from size limits and spool capacity. It is not an Ollama registry: the names, the auth, the blob `Location` rule and the list type each stop it.

---

## 8. Recommendation

Ranked by reach per unit of work, on the basis of the client facts above:

1. **Hugging Face, remote class first, read path without Xet (must).**
   - **Why first:** it is the origin of almost every weight local tools use. LM Studio, Jan, GPT4All and llama.cpp download from it, and Ollama's `hf.co` path ends in its `resolve` URLs.
   - **Reach:** one variable (`HF_ENDPOINT`, or `MODEL_ENDPOINT` for llama.cpp) points transformers, diffusers, vLLM, SGLang, mlx_lm, llama.cpp, LocalAI, RamaLama and NIM at it.
   - **Cost:** the plain-HTTP read path is a bounded API (section 6.1) and serves every file up to 50 GB. The catalogue page already exists.
   - **Add a host-root binding for the format at the same time:** Ollama's view needs it, and so would any TLS-intercepting setup for LM Studio.
2. **An Ollama format of its own (must).** It shares the OCI format's manifest parser, blob store, upload sessions and remote engine, and has:
   - a remote of registry.ollama.ai;
   - a local class;
   - the Ollama view on the Hugging Face format.

   Ollama is the most used local runner and has no mirror setting, so the mirror host in the model name is the only way in.

   It is a format of its own, not an OCI mode, because five things differ at the protocol level:
   - two-segment names on a host root;
   - the ed25519 key handshake (users register their Ollama public key);
   - a blob `GET` that must answer with a `Location` valid for the whole download;
   - the Ollama list type;
   - an upstream without `/v2/` and without a tags list.

   The setup screen must say that clients on a private network need `HTTPS_PROXY` (section 6.2).
3. **Hugging Face local and virtual**, to keep own fine-tunes.
   - With current clients this means the Xet write path. Without it, every uploader must set `HF_HUB_DISABLE_XET=1`.
   - The Xet read path comes with it and lifts the 50 GB limit.
4. **OCI model artifacts: no new format.** What is needed:
   - size settings and a spool sized for models;
   - the clients `docker model`, `modctl`, `kit` and `oras` added to the OCI format's tested clients;
   - optionally, a ModelPack config's architecture, parameter size and quantisation shown on the screens.
5. **Plain files: the generic format as it is**, for llamafile, KoboldCpp, `llama.cpp -mu`, and files to sideload into LM Studio, Jan and GPT4All.
6. **ModelScope only on demand.** No format for:
   - Kaggle: it cannot be mirrored;
   - the NGC CLI: there is no override, and NIM is reached through HF;
   - PyTorch Hub, TF Hub, MLflow and Triton.

**Separate formats:**
- **Hugging Face:** its own HTTP API, revisions, Xet.
- **Ollama:** OCI-like, but incompatible in names, auth and blob delivery.
- **ModelScope:** if ever.

**Covered as OCI artifacts:** Docker Model Runner (both packagings), ModelPack, KitOps, ORAS, RamaLama images, KServe modelcars, Cog weights.

---

## 9. Verified versus not verified

**Verified by me directly**, in addition to the sub-researchers' reading:
- the Ollama blob-download, redirect, auth, name and layer code, at `main` and at the release v0.40.18;
- `huggingface_hub`'s 50 GB cap, its header names, the tree-cache Xet shortcut and the Xet-only upload;
- llama.cpp's endpoints and endpoint variables;
- Docker Model Runner's and ModelPack's media types;
- live: HF `resolve` headers; the hf.co Ollama manifest and blob redirect; the GGUF's LFS SHA-256 being identical to the Ollama layer digest; the Docker Hub `ai/smollm2` manifests in both packagings;
- the open state of LM Studio #474, lms #104 and Ollama #7244;
- Quader's spec passages cited in section 7.

**Not verified; these are the gaps:**
- LM Studio's download code is closed; its endpoints are known from user logs only.
- Nothing was run against Quader. These come from the spec or are inferred:
  - how a remote fill treats an Ollama list manifest;
  - whether Ollama's anonymous pull of a public Quader repository succeeds;
  - whether the maximum upload size bounds a fill.
- The Hub's rule for `lfs` versus `regular` in `preupload`, and the status it gives an authenticated user without gated access.
- Whether Docker Model Runner's model pulls read the same mirror list as its backend images, and how it uploads.
- The status of Xet's migration of old Hub repositories: the docs state all models are Xet-backed, but I found no dated completion.
- NGC's formats and REST paths; LiteRT's file types. Civitai was not researched.
- Where sources disagree:
  - the Hub's tree page size: the client comment says 1,000, or 50 with `expand`; the OpenAPI spec says 1,000, or 100 with `expand`;
  - KitOps' spec page (`vnd.jozu.*`) against its code (`vnd.kitops.*`);
  - `HF_ENDPOINT` is read by the code but missing from the current env-var docs page.