# Open Cluster Management Addon Framework

A Go library that provides a framework for developing addons for Open Cluster Management (OCM). This framework simplifies the process of creating, installing, and managing addons across multiple Kubernetes clusters in an OCM environment.

## Overview

Open Cluster Management (OCM) is a Kubernetes-native solution for managing multiple clusters. The Addon Framework enables developers to build addons that can be deployed and managed across these clusters with minimal complexity. The framework handles addon lifecycle management, configuration, registration, and deployment patterns.

## Features

- **Multiple Deployment Methods**: Support for Go templates, Helm charts, and template-based deployments
- **Lifecycle Management**: Automated installation, upgrade, and removal of addons across managed clusters
- **Configuration Management**: Flexible configuration system supporting various configuration types
- **Hosting Modes**: Support for both standard and hosted deployment modes
- **Registration Framework**: Automatic addon registration with cluster management
- **RBAC Integration**: Built-in support for role-based access control
- **Multi-cluster Support**: Deploy addons across multiple managed clusters from a central hub

## Core Concepts

The framework is built around several key Kubernetes custom resources:

- **[ClusterManagementAddOn](https://github.com/open-cluster-management-io/api/blob/main/addon/v1alpha1/types_clustermanagementaddon.go)**: Hub cluster resource that defines addon metadata and installation strategy
- **[ManagedClusterAddOn](https://github.com/open-cluster-management-io/api/blob/main/addon/v1alpha1/types_managedclusteraddon.go)**: Managed cluster resource that represents addon installation state
- **AddOnTemplate**: Template-based addon deployment without dedicated controllers

## Getting Started

### Prerequisites

- One or more Kubernetes clusters
- [Open Cluster Management](https://github.com/open-cluster-management-io/registration-operator) installed and configured
- At least one managed cluster imported and accepted

### Installation

Add the framework to your Go module:

```bash
go get open-cluster-management.io/addon-framework
```

### Basic Usage

The framework supports multiple addon implementation patterns:

1. **Go Template Based**: Use Go templates to define addon resources
2. **Helm Chart Based**: Deploy addons using Helm charts
3. **Template Based**: Use AddOnTemplate for simplified deployment
4. **Hosted Mode**: Run addon agents on hosting clusters

For detailed examples and tutorials, see the [examples directory](examples/README.md).

For comprehensive documentation on OCM addons, refer to:
- [OCM Add-on Concepts](https://open-cluster-management.io/docs/concepts/add-on-extensibility/addon/) - Core concepts and architecture
- [OCM Add-on Developer Guide](https://open-cluster-management.io/docs/developer-guides/addon/) - Complete development guide

### Framework Documentation
- [Design Documentation](https://github.com/open-cluster-management-io/enhancements/tree/main/enhancements/sig-architecture/8-addon-framework)
- [Helm Agent Addon Guide](docs/helmAgentAddon.md)
- [Template Agent Addon Guide](docs/templateAgentAddon.md)
- [Pre-delete Hook Guide](docs/preDeleteHook.md)

## Examples

The repository includes several example addons:

- **HelloWorld**: Basic addon using Go templates
- **HelloWorld Helm**: Addon implemented with Helm charts
- **HelloWorld Hosted**: Addon with hosted deployment mode
- **Kubernetes Dashboard**: Real-world addon example

See [examples/README.md](examples/README.md) for detailed instructions.

## Community Addons

### Active Projects

- [cluster-proxy](https://github.com/open-cluster-management-io/cluster-proxy): Provides secure proxy access to managed clusters
- [managed-serviceaccount](https://github.com/open-cluster-management-io/managed-serviceaccount): Manages service accounts across clusters

### Contributed Addons

A [collection of community addons](https://github.com/open-cluster-management-io/addon-contrib) including:
AI integration, IoT layer, cluster proxy, telemetry, resource usage collection, and more.

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to contribute to this project.

## Support

For questions and support:
- Open an issue in this repository
- Join the Open Cluster Management community discussions
