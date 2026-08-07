# rf-clipboard

[![ci](https://github.com/RunFridge/rf-clipboard/actions/workflows/ci.yml/badge.svg)](https://github.com/RunFridge/rf-clipboard/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/RunFridge/rf-clipboard)](https://github.com/RunFridge/rf-clipboard/releases/latest)
[![go](https://img.shields.io/github/go-mod/go-version/RunFridge/rf-clipboard)](../go.mod)
[![license](https://img.shields.io/github/license/RunFridge/rf-clipboard)](../LICENSE)

[English](../README.md) | 한국어

셀프 호스팅 서버를 통해 UNIX 계열 기기 간에 동기화되는 CLI 공유 클립보드입니다.
내용은 서버가 절대 볼 수 없는 키로 클라이언트에서 암호화됩니다 — 서버는 익명
계정 ID 아래에 암호문만 저장합니다.

```sh
# 기기 A에서
echo 'hello world' | rf-clip

# 기기 B에서
rf-paste > file.txt
```

## 설치 (클라이언트)

다음 중 하나:

```sh
# 설치 스크립트 (최신 GitHub 릴리스를 내려받습니다)
curl -fsSL https://raw.githubusercontent.com/RunFridge/rf-clipboard/main/scripts/install.sh | sh

# go install (이후 rf-copy/rf-paste 심볼릭 링크를 만들거나 `rf-clip paste` 사용)
go install github.com/RunFridge/rf-clipboard/cmd/rf-clip@latest
ln -s "$(command -v rf-clip)" ~/.local/bin/rf-copy
ln -s "$(command -v rf-clip)" ~/.local/bin/rf-paste

# 또는 릴리스 페이지에서 바이너리를 직접 받으세요
```

설치 스크립트는 두 심볼릭 링크를 모두 만듭니다. `rf-copy`는 `rf-clip`의
별칭이고(둘 다 stdin을 복사), `rf-paste`는 붙여넣습니다.

## 초기 설정

```sh
rf-clip init
```

기본으로 호스팅 서버 `https://clip.runfridge.dev`를 사용합니다. 셀프
호스팅한다면 init을 자신의 서버로 향하게 하세요:

```sh
SERVER_URL=https://clip.example.com rf-clip init
```

`init`은 무작위 256비트 시크릿을 생성해
`$XDG_CONFIG_HOME/rf-clipboard.conf`(기본 `~/.config/rf-clipboard.conf`,
권한 0600)에 기록합니다. 이 파일을 다른 기기로 복사하면 클립보드가
공유됩니다 — 시크릿을 공유하는 것이 곧 클립보드를 공유하는 것입니다.
시크릿을 잃으면 서버 쪽 데이터에 접근할 수 없게 되므로, `init`은 `-f`를
주지 않는 한 기존 설정 파일을 덮어쓰지 않습니다.

## 암호화 동작 방식

하나의 시크릿에서 두 값을 파생합니다(HKDF-SHA256):

- **계정 ID** = `HKDF(secret, "rf-clipboard/account-id")` — Bearer 토큰으로
  서버에 전송됩니다. 해시이므로 서버는 여기서 아무것도 알아낼 수 없고,
  가입 절차도 없습니다. 형식만 맞으면 어떤 ID든 계정입니다.
- **암호화 키** = `HKDF(secret, "rf-clipboard/encryption-key")` —
  클라이언트에서 AES-256-GCM에 사용됩니다. 절대 기기를 떠나지 않습니다.

서버가 침해되어도 얻는 것은 암호문과 불투명한 ID뿐입니다.

계정 ID는 동시에 *쓰기* 권한이기도 합니다. 이를 가진 누구든 — 서버
운영자를 포함해 — 클립보드를 덮어쓰거나 삭제하거나 과거 암호문을 재전송할
수 있습니다. 하지만 내용을 읽거나 위조할 수는 없습니다(당신의 키로
암호화되지 않은 데이터는 GCM 인증에 실패합니다). 기밀성과 내용 무결성은
보장되지만, 가용성은 보안 모델에 포함되지 않습니다.

## 서버 셀프 호스팅

### Docker (권장)

```sh
docker run -d --name rf-clipd --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v rf-clipd-data:/data \
  -e RF_CLIPD_PERSIST=/data/snap.gob \
  ghcr.io/runfridge/rf-clipd:latest
```

이미지는 릴리스마다 `ghcr.io/runfridge/rf-clipd`(linux amd64/arm64)로
게시되며, `latest`와 릴리스 버전 태그가 붙습니다.

또는 샘플 [docker-compose.yml](../docker-compose.yml)을 사용하세요:

```sh
docker compose up -d
```

그 앞에 TLS를 종단하는 리버스 프록시를 두세요. 예: Caddy:

```
clip.example.com {
    reverse_proxy localhost:8080
}
```

마지막으로 각 클라이언트를 서버로 향하게 합니다:

```sh
SERVER_URL=https://clip.example.com rf-clip init
```

### 바이너리 직접 실행

릴리스 페이지에서 `rf-clipd_<os>_<arch>`를 내려받거나(소스에서는
`make server`) 프로세스 관리자 아래에서 실행하세요:

```sh
rf-clipd -addr :8080 -ttl 24h -max-size 1048576 -max-entries 1000 -persist /var/lib/rf-clipd/snap.gob
```

모든 플래그는 환경 변수로도 읽습니다: `RF_CLIPD_ADDR`, `RF_CLIPD_TTL`,
`RF_CLIPD_MAX_SIZE`, `RF_CLIPD_MAX_ENTRIES`, `RF_CLIPD_PERSIST`(플래그가
우선).

### 참고 사항

- 저장소는 인메모리 맵입니다. `-ttl` 동안 사용되지 않은(복사도 붙여넣기도
  없는) 항목은 주기적 스윕으로 제거됩니다.
- `-persist`(선택)는 종료 시와 매 스윕 틱마다 맵을 파일로 스냅샷하고 시작
  시 다시 불러옵니다 — 재부팅에도 데이터가 살아남고, 크래시 시 손실은
  최대 한 스윕 간격입니다. 파일에는 암호문만 들어 있습니다.
- 저장소가 `max-entries`에 도달하면 기존 항목을 밀어내는 대신 *새* 계정을
  거부합니다(507). 이는 의도된 동작입니다: ID를 무작위로 뿌리는 스팸이
  TTL로 정리될 때까지 새 계정을 막을 수는 있어도, 당신의 데이터를 밀어낼
  수는 없습니다. "store full" 로그가 보이면 `-max-entries`를 올리세요.
- **용량 산정:** 최악의 경우 메모리 ≈ `max-entries × max-size`(기본값
  기준 약 1 GB). 이 곱이 호스트 RAM에 들어가도록 상한을 정하세요. 일반적인
  개인 사용은 총 몇 KB 수준이라 작은 VPS로도 충분합니다.
- 서버는 평문 HTTP로 통신합니다. TLS를 종단하는 리버스 프록시(Caddy,
  nginx, Traefik) 뒤에서 실행하세요 — 계정 토큰이 헤더로 전달되고, 클립
  내용은 이미 클라이언트에서 암호화되어 있습니다.

### API

| 메서드 | 경로       | 인증                       | 응답                               |
| ------ | ---------- | -------------------------- | ---------------------------------- |
| PUT    | `/v1/clip` | `Bearer <64자리 hex 계정>` | 204, 413 크기 초과, 507 저장소 가득 참 |
| GET    | `/v1/clip` | `Bearer <64자리 hex 계정>` | 200 암호문, 404 비어 있음          |
