{
  local otel = self,

  namespace:: error 'must provide namespace',
  image:: error 'must provide image',
  jaegerEndpoint:: error 'must provide jaeger endpoint',

  agent:: {
    resources: {
      requests: {
        cpu: '3m',
        memory: '50Mi',
      },
      limits: {
        cpu: '30m',
        memory: '100Mi',
      },
    },
  },

  collector:: {
    resources: {
      requests: {
        cpu: '3m',
        memory: '40Mi',
      },
      limits: {
        cpu: '30m',
        memory: '100Mi',
      },
    },
  },

  agentConfigmap: {
    apiVersion: 'v1',
    kind: 'ConfigMap',
    metadata: {
      labels: {
        app: 'opentelemetry',
        component: 'otel-agent-conf',
      },
      name: 'otel-agent-conf',
      namespace: otel.namespace,
    },
    data: {
      'otel-agent-config': |||
        receivers:
          otlp:
            protocols:
              grpc:
              http:
          jaeger:
            protocols:
              grpc:
              thrift_binary:
              thrift_compact:
              thrift_http:
        exporters:
          otlp:
            endpoint: "otel-collector.%s:4317"
            tls:
              insecure: true
            sending_queue:
              num_consumers: 4
              queue_size: 100
            retry_on_failure:
              enabled: true
        processors:
          batch:
          memory_limiter:
            limit_mib: 400
            spike_limit_mib: 100
            check_interval: 5s
        extensions:
          health_check: {}
          zpages: {}
        service:
          extensions: [health_check, zpages]
          pipelines:
            traces:
              receivers: [otlp]
              processors: [memory_limiter, batch]
              exporters: [otlp]
      ||| % otel.namespace,
    },
  },

  agentDaemonset: {
    apiVersion: 'apps/v1',
    kind: 'DaemonSet',
    metadata: {
      labels: {
        app: 'opentelemetry',
        component: 'otel-agent',
      },
      name: 'otel-agent',
      namespace: otel.namespace,
    },
    spec: {
      selector: {
        matchLabels: {
          app: 'opentelemetry',
          component: 'otel-agent',
        },
      },
      template: {
        metadata: {
          labels: {
            app: 'opentelemetry',
            component: 'otel-agent',
          },
        },
        spec: {
          serviceAccountName: otel.agentServiceAccount.metadata.name,
          containers: [
            {
              command: [
                '/otelcol',
                '--config=/conf/otel-agent-config.yaml',
              ],
              image: otel.image,
              livenessProbe: {
                httpGet: {
                  path: '/',
                  port: 13133,
                },
              },
              name: 'otel-agent',
              ports: [
                {
                  containerPort: 55679,
                },
                {
                  containerPort: 4317,
                  hostPort: 4317,
                },
                {
                  containerPort: 8888,
                },
              ],
              readinessProbe: {
                httpGet: {
                  path: '/',
                  port: 13133,
                },
              },
              resources: otel.agent.resources,
              volumeMounts: [
                {
                  mountPath: '/conf',
                  name: 'otel-agent-config-vol',
                },
              ],
            },
          ],
          volumes: [
            {
              configMap: {
                items: [
                  {
                    key: 'otel-agent-config',
                    path: 'otel-agent-config.yaml',
                  },
                ],
                name: 'otel-agent-conf',
              },
              name: 'otel-agent-config-vol',
            },
          ],
        },
      },
    },
  },

  agentServiceAccount: {
    apiVersion: 'v1',
    kind: 'ServiceAccount',
    metadata: {
      name: 'otel-agent',
      namespace: otel.namespace,
    },
  },

  agentPodSecurityPolicy: {
    apiVersion: 'policy/v1beta1',
    kind: 'PodSecurityPolicy',
    metadata: {
      name: 'otel-agent',
    },
    spec: {
      allowedHostPaths: [
        {
          pathPrefix: '/proc',
          readOnly: true,
        },
        {
          pathPrefix: '/sys',
          readOnly: true,
        },
        {
          pathPrefix: '/',
          readOnly: true,
        },
      ],
      allowPrivilegeEscalation: false,
      fsGroup: {
        ranges: [
          {
            max: 65535,
            min: 1,
          },
        ],
        rule: 'MustRunAs',
      },
      hostIPC: false,
      hostNetwork: true,
      hostPID: true,
      hostPorts: [
        {
          max: 65535,
          min: 1,
        },
      ],
      privileged: false,
      readOnlyRootFilesystem: false,
      requiredDropCapabilities: [
        'ALL',
      ],
      runAsUser: {
        rule: 'RunAsAny',
      },
      seLinux: {
        rule: 'RunAsAny',
      },
      supplementalGroups: {
        ranges: [
          {
            max: 65535,
            min: 1,
          },
        ],
        rule: 'MustRunAs',
      },
      volumes: [
        'configMap',
        'hostPath',
        'secret',
      ],
    },
  },

  agentRole: {
    apiVersion: 'rbac.authorization.k8s.io/v1',
    kind: 'Role',
    metadata: {
      name: 'otel-agent',
      namespace: otel.namespace,
    },
    rules: [
      {
        apiGroups: [
          'policy',
        ],
        resourceNames: [
          'otel-agent',
        ],
        resources: [
          'podsecuritypolicies',
        ],
        verbs: [
          'use',
        ],
      },
    ],
  },

  agentRoleBinding: {
    apiVersion: 'rbac.authorization.k8s.io/v1',
    kind: 'RoleBinding',
    metadata: {
      name: 'otel-agent',
      namespace: otel.namespace,
    },
    roleRef: {
      apiGroup: 'rbac.authorization.k8s.io',
      kind: 'Role',
      name: otel.agentRole.metadata.name,
    },
    subjects: [
      {
        kind: 'ServiceAccount',
        name: otel.agentServiceAccount.metadata.name,
      },
    ],
  },

  collectorConfigmap: {
    apiVersion: 'v1',
    kind: 'ConfigMap',
    metadata: {
      labels: {
        app: 'opentelemetry',
        component: 'otel-collector-conf',
      },
      name: 'otel-collector-conf',
      namespace: otel.namespace,
    },
    data: {
      'otel-collector-config': |||
        receivers:
          otlp:
            protocols:
              grpc:
              http:
          jaeger:
            protocols:
              grpc:
              thrift_http:
          zipkin: {}
        processors:
          batch:
          memory_limiter:
            limit_mib: 1500
            spike_limit_mib: 512
            check_interval: 5s
        extensions:
          health_check: {}
          zpages: {}
        exporters:
          jaeger:
            endpoint: %s
            tls:
              insecure: true
        service:
          extensions: [health_check, zpages]
          pipelines:
            traces:
              receivers: [otlp, jaeger]
              processors: [memory_limiter, batch]
              exporters: [jaeger]
      ||| % otel.jaegerEndpoint,
    },
  },

  collectorService: {
    apiVersion: 'v1',
    kind: 'Service',
    metadata: {
      labels: {
        app: 'opentelemetry',
        component: 'otel-collector',
      },
      name: 'otel-collector',
      namespace: otel.namespace,
    },
    spec: {
      ports: [
        {
          name: 'otlp',
          port: 4317,
          protocol: 'TCP',
          targetPort: 4317,
        },
      ],
      selector: {
        component: 'otel-collector',
      },
      type: 'NodePort',
    },
  },

  collectorServiceAccount: {
    apiVersion: 'v1',
    kind: 'ServiceAccount',
    metadata: {
      name: 'otel-collector',
      namespace: otel.namespace,
    },
  },

  collectorRole: {
    apiVersion: 'rbac.authorization.k8s.io/v1',
    kind: 'Role',
    metadata: {
      name: 'otel-collector',
      namespace: otel.namespace,
    },
    rules: [
      {
        apiGroups: [
          'policy',
        ],
        resourceNames: [
          'restricted',
        ],
        resources: [
          'podsecuritypolicies',
        ],
        verbs: [
          'use',
        ],
      },
    ],
  },

  collectorRoleBinding: {
    apiVersion: 'rbac.authorization.k8s.io/v1',
    kind: 'RoleBinding',
    metadata: {
      name: 'otel-collector',
      namespace: otel.namespace,
    },
    roleRef: {
      apiGroup: 'rbac.authorization.k8s.io',
      kind: 'Role',
      name: otel.collectorRole.metadata.name,
    },
    subjects: [
      {
        kind: 'ServiceAccount',
        name: otel.collectorServiceAccount.metadata.name,
      },
    ],
  },

  collectorDeployment: {
    apiVersion: 'apps/v1',
    kind: 'Deployment',
    metadata: {
      labels: {
        app: 'opentelemetry',
        component: 'otel-collector',
      },
      name: 'otel-collector',
      namespace: otel.namespace,
    },
    spec: {
      minReadySeconds: 5,
      progressDeadlineSeconds: 120,
      replicas: 1,
      selector: {
        matchLabels: {
          app: 'opentelemetry',
          component: 'otel-collector',
        },
      },
      template: {
        metadata: {
          labels: {
            app: 'opentelemetry',
            component: 'otel-collector',
          },
        },
        spec: {
          serviceAccountName: otel.collectorServiceAccount.metadata.name,
          containers: [
            {
              command: [
                '/otelcol',
                '--config=/conf/otel-collector-config.yaml',
              ],
              env: [
                {
                  name: 'GOGC',
                  value: '80',
                },
              ],
              image: otel.image,
              livenessProbe: {
                httpGet: {
                  path: '/',
                  port: 13133,
                },
              },
              name: 'otel-collector',
              securityContext: {
                runAsUser: 65534,
              },
              ports: [
                {
                  containerPort: 4317,
                },
              ],
              readinessProbe: {
                httpGet: {
                  path: '/',
                  port: 13133,
                },
              },
              resources: otel.collector.resources,
              volumeMounts: [
                {
                  mountPath: '/conf',
                  name: 'otel-collector-config-vol',
                },
              ],
            },
          ],
          volumes: [
            {
              name: 'otel-collector-config-vol',
              configMap: {
                items: [
                  {
                    key: 'otel-collector-config',
                    path: 'otel-collector-config.yaml',
                  },
                ],
                name: 'otel-collector-conf',
              },
            },
          ],
        },
      },
    },
  },
}
