# HLS Converter Service (Go)

Serviço de conversão de vídeos para HLS (HTTP Live Streaming) escrito em Go. Recebe requisições de uma aplicação Laravel, baixa o vídeo original do S3, converte para HLS usando FFmpeg em múltiplas qualidades, faz upload dos segmentos de volta ao S3 e envia callbacks para a aplicação Laravel.

## Pré-requisitos

- **Go 1.21+** — [Instalar Go](https://go.dev/doc/install)
- **FFmpeg** — Deve estar disponível no PATH (ou configurar `FFMPEG_PATH`)
  ```bash
  # Ubuntu/Debian
  sudo apt install ffmpeg
  
  # macOS
  brew install ffmpeg
  ```
- **Credenciais AWS** — Acesso ao S3 com permissões de leitura e escrita no bucket configurado

## Como rodar localmente

### 1. Clonar e configurar

```bash
cd hls-go
cp .env.example .env
```

Edite o arquivo `.env` com suas credenciais:

```env
PORT=8001
AWS_ACCESS_KEY_ID=sua-access-key
AWS_SECRET_ACCESS_KEY=sua-secret-key
AWS_DEFAULT_REGION=us-east-1
AWS_BUCKET=seu-bucket
FFMPEG_PATH=ffmpeg
TEMP_DIR=/tmp/hls-conversions
```

### 2. Instalar dependências

```bash
go mod tidy
```

### 3. Rodar o serviço

```bash
go run .
```

O servidor irá iniciar na porta configurada (padrão: 8001).

### 4. Rodar com hot reload (opcional)

```bash
# Instalar air
go install github.com/air-verse/air@latest

# Rodar
air
```

## Como buildar

```bash
go build -o hls-converter .
./hls-converter
```

## Deploy no ECS

### 1. Build e push da imagem Docker

```bash
# Build local
docker build -t hls-converter .

# Tag para ECR
docker tag hls-converter:latest <ACCOUNT_ID>.dkr.ecr.<REGION>.amazonaws.com/hls-converter:latest

# Login no ECR
aws ecr get-login-password --region <REGION> | docker login --username AWS --password-stdin <ACCOUNT_ID>.dkr.ecr.<REGION>.amazonaws.com

# Push
docker push <ACCOUNT_ID>.dkr.ecr.<REGION>.amazonaws.com/hls-converter:latest
```

### 2. Task Definition exemplo

```json
{
  "family": "hls-converter",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "2048",
  "memory": "4096",
  "containerDefinitions": [
    {
      "name": "hls-converter",
      "image": "<ACCOUNT_ID>.dkr.ecr.<REGION>.amazonaws.com/hls-converter:latest",
      "portMappings": [
        {
          "containerPort": 8001,
          "protocol": "tcp"
        }
      ],
      "environment": [
        { "name": "PORT", "value": "8001" },
        { "name": "AWS_DEFAULT_REGION", "value": "us-east-1" },
        { "name": "AWS_BUCKET", "value": "seu-bucket" },
        { "name": "TEMP_DIR", "value": "/tmp/hls-conversions" }
      ],
      "secrets": [
        {
          "name": "AWS_ACCESS_KEY_ID",
          "valueFrom": "arn:aws:secretsmanager:<REGION>:<ACCOUNT_ID>:secret:hls-converter:AWS_ACCESS_KEY_ID::"
        },
        {
          "name": "AWS_SECRET_ACCESS_KEY",
          "valueFrom": "arn:aws:secretsmanager:<REGION>:<ACCOUNT_ID>:secret:hls-converter:AWS_SECRET_ACCESS_KEY::"
        }
      ],
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "/ecs/hls-converter",
          "awslogs-region": "<REGION>",
          "awslogs-stream-prefix": "ecs"
        }
      }
    }
  ],
  "executionRoleArn": "arn:aws:iam::<ACCOUNT_ID>:role/ecsTaskExecutionRole",
  "taskRoleArn": "arn:aws:iam::<ACCOUNT_ID>:role/ecsTaskRole"
}
```

### 3. Variáveis de ambiente no ECS

| Variável | Obrigatória | Descrição |
|----------|-------------|-----------|
| `PORT` | Não | Porta do servidor HTTP (padrão: 8001) |
| `AWS_ACCESS_KEY_ID` | Sim* | Chave de acesso AWS |
| `AWS_SECRET_ACCESS_KEY` | Sim* | Chave secreta AWS |
| `AWS_DEFAULT_REGION` | Não | Região AWS (padrão: us-east-1) |
| `AWS_BUCKET` | Sim | Nome do bucket S3 |
| `FFMPEG_PATH` | Não | Caminho do FFmpeg (padrão: ffmpeg) |
| `TEMP_DIR` | Não | Diretório temporário (padrão: /tmp/hls-conversions) |

*No ECS, pode-se usar a IAM Role da task ao invés de credenciais explícitas.

## Endpoints da API

### POST /api/hls/convert

Inicia uma conversão de vídeo para HLS.

**Request:**
```json
{
  "media_file_id": 123,
  "title": "Título do Vídeo",
  "s3_path": "videos/abc123/original.mp4",
  "width": 1920,
  "height": 1080,
  "duration": 3600,
  "qualities": ["360p", "480p", "720p", "1080p"],
  "callback_url": "https://exemplo.com/api/hls/callback",
  "cloudfront_url": "https://dxxxxx.cloudfront.net/videos/abc123/original.mp4"
}
```

**Response (202 Accepted):**
```json
{
  "conversion_id": "uuid-string",
  "message": "Conversão iniciada"
}
```

### DELETE /api/hls/{conversion_id}

Cancela uma conversão em andamento.

**Response (200):**
```json
{
  "success": true,
  "message": "Conversão cancelada"
}
```

### GET /api/hls/health

Health check do serviço.

**Response (200):**
```json
{
  "status": "ok"
}
```

### Callback (enviado pelo serviço)

Quando cada qualidade termina, o serviço envia um POST para a `callback_url`:

**Sucesso:**
```json
{
  "media_id": 123,
  "quality": "720p",
  "status": "completed",
  "s3_path": "videos/123/hls/720p/master.m3u8"
}
```

**Falha:**
```json
{
  "media_id": 123,
  "quality": "720p",
  "status": "failed",
  "error_message": "detalhes do erro"
}
```

## Como testar

### Health check

```bash
curl http://localhost:8001/api/hls/health
```

### Iniciar conversão

```bash
curl -X POST http://localhost:8001/api/hls/convert \
  -H "Content-Type: application/json" \
  -d '{
    "media_file_id": 1,
    "title": "Teste",
    "s3_path": "videos/teste/original.mp4",
    "width": 1920,
    "height": 1080,
    "duration": 60,
    "qualities": ["360p", "720p"],
    "callback_url": "https://webhook.site/seu-uuid",
    "cloudfront_url": "https://dxxxxx.cloudfront.net/videos/teste/original.mp4"
  }'
```

### Cancelar conversão

```bash
curl -X DELETE http://localhost:8001/api/hls/{conversion_id}
```

### Testar com webhook.site

1. Acesse [webhook.site](https://webhook.site) e copie sua URL única
2. Use essa URL como `callback_url` na requisição de conversão
3. Acompanhe os callbacks recebidos no webhook.site

## Qualidades suportadas

| Qualidade | Resolução | Bitrate | Max Rate | Buffer Size |
|-----------|-----------|---------|----------|-------------|
| 360p | 640x360 | 800k | 856k | 1200k |
| 480p | 854x480 | 1400k | 1498k | 2100k |
| 720p | 1280x720 | 2800k | 2996k | 4200k |
| 1080p | 1920x1080 | 5000k | 5350k | 7500k |
| 1440p | 2560x1440 | 8000k | 8560k | 12000k |
| 2160p | 3840x2160 | 14000k | 14980k | 21000k |
