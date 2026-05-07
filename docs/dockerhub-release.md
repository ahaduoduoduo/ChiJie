# Docker Hub 自动发布

日期：2026-05-07

## 目标

GitHub Actions 在 `main` 分支更新后自动构建 Linux AMD64 镜像并推送到 Docker Hub。VPS 只拉取镜像运行，不再在服务器上执行 Node / Go / buildx 构建。

默认镜像名：

```text
ahaduoduoduo/chijie:latest
```

如果 Docker Hub 用户名不是 `ahaduoduoduo`，只需要调整 `.env` 的 `CHIJIE_IMAGE`。

## GitHub Secrets

仓库地址：

```text
https://github.com/ahaduoduoduo/ChiJie
```

在 GitHub 仓库中添加两个 Actions Secrets：

```text
DOCKERHUB_USERNAME
DOCKERHUB_TOKEN
```

`DOCKERHUB_USERNAME` 填 Docker Hub 用户名。`DOCKERHUB_TOKEN` 填 Docker Hub Access Token，权限需要 `Read & Write`。

## Workflow

工作流文件：

```text
.github/workflows/dockerhub.yml
```

触发方式：

- 推送到 `main` 分支。
- GitHub Actions 页面手动运行 `Docker Hub Image`。

输出标签：

```text
<dockerhub-username>/chijie:latest
<dockerhub-username>/chijie:<git-commit-sha>
```

当前只构建 `linux/amd64`，匹配 X86 VPS。

## VPS 运行

生产环境使用拉镜像专用 Compose 文件：

```bash
cp .env.example .env
cp configs/gateway.docker.yaml.example configs/gateway.yaml
cp configs/nodes.yaml.example configs/nodes.yaml
cp configs/fingerprints.yaml.example configs/fingerprints.yaml
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

`.env` 推荐配置：

```env
CHIJIE_IMAGE=ahaduoduoduo/chijie:latest
CHIJIE_PROXY_HOST=127.0.0.1
CHIJIE_PROXY_PORT=18080
CHIJIE_ADMIN_HOST=127.0.0.1
CHIJIE_ADMIN_PORT=19090
CHIJIE_CONFIG_DIR=./configs
TZ=Asia/Shanghai
```

nginx 反向代理到：

```text
http://127.0.0.1:18080
```

Admin 继续使用 SSH tunnel：

```bash
ssh -L 9090:127.0.0.1:19090 root@your-vps
```

## 更新

新代码推送到 GitHub 后，等待 Actions 构建成功。VPS 执行：

```bash
cd /www/wwwroot/proxy.infiniteapi.com/app
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
docker compose -f docker-compose.prod.yml logs -f chijie
```

配置文件挂载自宿主机 `configs/`，镜像更新不会覆盖 `gateway.yaml`、`nodes.yaml` 和 `fingerprints.yaml`。
