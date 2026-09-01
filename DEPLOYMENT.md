# Rancher Deployment Guide

This guide explains how to deploy the scheduling app to Rancher using Docker images built by GitHub Actions.

## Prerequisites

- Rancher cluster running (local K3s or production)
- GitHub repository with GitHub Container Registry (GHCR) access
- `kubectl` configured to access your Rancher cluster
- Images pushed to GHCR by GitHub Actions (automatic on push to main/develop)

## Step 1: Prepare registry credentials (if images are private)

If you haven't made your GitHub images public:

```bash
kubectl create secret docker-registry ghcr-secret \
  --docker-server=ghcr.io \
  --docker-username=<github-username> \
  --docker-password=<github-personal-access-token> \
  --docker-email=<your-email> \
  -n default
```

## Step 2: Deploy via Rancher UI

### Option A: Using Rancher Dashboard

1. **Log into Rancher**
2. Go to **Cluster → Local** (or your target cluster)
3. Click **Workloads → Deployments → Create**
4. Fill in the deployment details:

   **Backend Deployment:**
   - Name: `scheduling-backend`
   - Image: `ghcr.io/<your-username>/scheduling-backend:latest`
   - Image Pull Secret: `ghcr-secret` (if private)
   - Ports: Container Port 8080, Protocol TCP
   - Environment: Add `DATABASE_URL=postgres://user:pass@postgres-service:5432/scheduling`
   - Health: Enable HTTP health check at port 8080

   **Frontend Deployment:**
   - Name: `scheduling-frontend`
   - Image: `ghcr.io/<your-username>/scheduling-frontend:latest`
   - Image Pull Secret: `ghcr-secret` (if private)
   - Ports: Container Port 3000, Protocol TCP
   - Health: Enable HTTP health check at port 3000 to `/health`

### Option B: Using kubectl with YAML

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: scheduling-backend
spec:
  replicas: 1
  selector:
    matchLabels:
      app: scheduling-backend
  template:
    metadata:
      labels:
        app: scheduling-backend
    spec:
      imagePullSecrets:
        - name: ghcr-secret
      containers:
      - name: backend
        image: ghcr.io/<your-username>/scheduling-backend:latest
        ports:
        - containerPort: 8080
        env:
        - name: DATABASE_URL
          value: "postgres://user:pass@postgres-service:5432/scheduling"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10

---
apiVersion: v1
kind: Service
metadata:
  name: scheduling-backend
spec:
  selector:
    app: scheduling-backend
  ports:
  - port: 8080
    targetPort: 8080
  type: ClusterIP

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: scheduling-frontend
spec:
  replicas: 1
  selector:
    matchLabels:
      app: scheduling-frontend
  template:
    metadata:
      labels:
        app: scheduling-frontend
    spec:
      imagePullSecrets:
        - name: ghcr-secret
      containers:
      - name: frontend
        image: ghcr.io/<your-username>/scheduling-frontend:latest
        ports:
        - containerPort: 3000
        livenessProbe:
          httpGet:
            path: /health
            port: 3000
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /health
            port: 3000
          initialDelaySeconds: 5
          periodSeconds: 10

---
apiVersion: v1
kind: Service
metadata:
  name: scheduling-frontend
spec:
  selector:
    app: scheduling-frontend
  ports:
  - port: 3000
    targetPort: 3000
  type: LoadBalancer  # Or ClusterIP if behind ingress
```

## Step 3: Set up PostgreSQL

Either:
- Use managed database (AWS RDS, Cloud SQL)
- Deploy PostgreSQL in Rancher:

```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm install postgres bitnami/postgresql \
  --set auth.password=<password> \
  -n default
```

## Step 4: Set up Ingress (optional, for external access)

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: scheduling-ingress
spec:
  rules:
  - host: scheduling.example.com
    http:
      paths:
      - path: /api
        pathType: Prefix
        backend:
          service:
            name: scheduling-backend
            port:
              number: 8080
      - path: /
        pathType: Prefix
        backend:
          service:
            name: scheduling-frontend
            port:
              number: 3000
```

## Step 5: Deploy in Rancher

Apply the YAML:
```bash
kubectl apply -f deployment.yaml
```

Or use Rancher UI:
- Click **Import YAML** and paste the configuration

## Monitoring & Troubleshooting

### Check deployment status
```bash
kubectl get deployments
kubectl get pods
kubectl logs deployment/scheduling-backend
```

### Check if images are pulling
```bash
kubectl describe pod <pod-name>
```

### Verify services are running
```bash
kubectl get svc
kubectl get endpoints
```

## Auto-update images

To automatically pull latest images:
1. Install Rancher's **Image Update Automation** controller
2. Or set `imagePullPolicy: Always` in deployments
3. Or use GitOps (ArgoCD/Flux) to sync from GitHub

## Scaling

In Rancher UI:
- Click deployment → **Scale** → adjust replicas

Via kubectl:
```bash
kubectl scale deployment/scheduling-backend --replicas=3
```

## Rolling updates

When you push new code:
1. GitHub Actions builds and pushes image to `ghcr.io/.../scheduling-backend:latest`
2. Rancher pulls the new image (if `imagePullPolicy: Always`)
3. Pods automatically restart with new version
