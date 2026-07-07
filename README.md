<div align="center">
  <img src="https://img.icons8.com/color/96/000000/network-cable.png" alt="Logo" width="80"/>
  <h1>Infra/DevOps Doctor</h1>
  <p><b>A zero-setup, local-first Multi-Cloud Visualizer & Security Scanner</b></p>
  <p>
    <img src="https://img.shields.io/badge/build-passing-brightgreen" alt="Build Status"/>
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="License"/>
    <img src="https://img.shields.io/badge/platform-windows%20%7C%20linux%20%7C%20macOS-lightgrey" alt="Platform"/>
    <img src="https://img.shields.io/badge/go-1.21+-00ADD8?logo=go" alt="Go Version"/>
  </p>
</div>

---

**Infra/DevOps Doctor** (formerly InfraMap) is a powerful, single-binary developer tool that auto-discovers your live infrastructure across AWS, Azure, GCP, and Docker, and renders it as a highly interactive, dynamic D3.js graph. 

It sits entirely on your local machine—no SaaS subscriptions, no handing over IAM keys to a third-party server, and no complex microservices to deploy. It combines beautiful architecture visualization with an offline Cloud Security Posture Management (CSPM) engine.

## ✨ Features

- 🌍 **Unified Multi-Cloud Discovery**: Native support for **AWS, Azure, GCP, and Docker**. Scans your resources concurrently and merges them into a single "Mega-Graph".
- 🕸️ **Interactive D3.js Visualization**: Beautiful, themeable (Dark/Light) architecture map. Features a collapsible hierarchy tree, minimap, and progressive "Escape-key" navigation.
- 🎯 **Spotlight Mode (Blast Radius)**: Click any resource to instantly dim the map and highlight its blast radius. See exactly which web servers, load balancers, and subnets and exposed database is connected to.
- 🩺 **The "Doctor" (Offline Security Scanner)**: A zero-API-cost CSPM engine that analyzes your infrastructure graph entirely offline. It instantly flags:
  - Unencrypted S3/GCS buckets
  - Open SSH/RDP/Database ports (`0.0.0.0/0`)
  - Overly permissive IAM roles (`FullAccess`)
  - Privileged Docker containers & missing Multi-AZ deployments
  - Stopped/Orphaned resources silently racking up costs
- 💰 **Cost Estimation**: Overlays estimated monthly compute costs directly on the graph nodes.
- ⏪ **Time Travel**: Takes automatic snapshots of your infrastructure. Select a past snapshot to visually diff your graph (see exactly what was added, modified, or removed).
- 🏗️ **Terraform Export**: Right-click your live infrastructure to instantly generate and download the equivalent `main.tf` Terraform code.

---

## 🚀 Getting Started

### Prerequisites
- Go 1.21+
- You must be authenticated with your cloud provider locally:
  - **AWS**: Configure via `aws configure` (or `~/.aws/credentials`).
  - **Azure**: Login via `az login`.
  - **GCP**: Login via `gcloud auth application-default login`.
  - **Docker**: Have Docker Desktop running.

### Installation

The absolute easiest way to get started is to download the pre-compiled binary. There are **zero dependencies** to install (you don't even need Go!).

1. Go to the [Releases Page](https://github.com/AnuragJosyula/infra-devops-doctor/releases/latest).
2. Download the executable for your OS (`.exe` for Windows, or the Mac/Linux binaries).
3. Run it directly from your terminal!

### Build from Source (Optional)
If you prefer to compile it yourself (the frontend UI is embedded directly into the executable!):

```bash
git clone https://github.com/AnuragJosyula/infra-devops-doctor.git
cd infra-devops-doctor/cmd/inframap
go build -o doctor.exe .
```

### Running the App
Discover your infrastructure and launch the local web dashboard:

```bash
# Auto-detects your credentials and runs available providers
./doctor.exe serve

# Explicitly specify clouds to scan
./doctor.exe serve --providers=aws,azure,gcp

# Run on a custom port
./doctor.exe serve --port=8080
```
Open your browser to `http://localhost:8080` (or your chosen port).

---

## 🎮 Navigating the UI

I spent a lot of time polishing the UX to make it feel like a premium tool:

- **Left-Click a Node**: Opens the details sidebar, showing metadata, security findings, and exact connections. It also triggers **Spotlight Mode**, highlighting the immediate blast radius.
- **Right-Click a Node**: Opens the context menu to copy IDs, expand/collapse children, or trigger a deep Blast Radius analysis.
- **The "Escape" Key**: Acts as a progressive "Back" button. Hammering the Escape key will sequentially: close context menus -> clear blast radius -> close details -> clear spotlight -> collapse the current node -> zoom to fit the entire map.
- **Home Button**: Instantly collapses all deep dives and resets the view to the top-level multi-cloud map.

---

## 🛠️ Architecture

- **Backend**: Written in Go. Uses `aws-sdk-go-v2`, and shells out to `az` and `gcloud` CLIs for discovery. The backend serves a JSON graph via a local HTTP/WebSocket API.
- **Frontend**: Pure HTML, Vanilla CSS, and JS (`app.js`). Powered by `D3.js` for force-directed and tree graph rendering. The frontend assets are compiled into the Go binary using the `embed` package.

---

## 🤝 Contributing

Contributions are welcome! If you want to add a new security rule to the `doctor.go` scanner, or build a new cloud provider, please submit a Pull Request.

## 📄 License

This project is licensed under the MIT License - see the LICENSE file for details.
