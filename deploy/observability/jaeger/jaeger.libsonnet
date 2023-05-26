{
  local jaeger = self,
  namespace:: error 'must set namespace for jaeger',
  image:: error 'must set image for jaeger',
  queryBasePath:: '',
  replicas:: 1,

  service: {
    apiVersion: 'v1',
    kind: 'Service',
    metadata: {
      name: 'jaeger-collector',
      namespace: jaeger.namespace,
      labels: { 'app.kubernetes.io/name': jaeger.deployment.metadata.name },
    },
    spec: {
      ports: [
        { name: 'grpc', targetPort: 14250, port: 14250 },
      ],
      selector: jaeger.deployment.metadata.labels,
    },
  },

  queryService: {
    apiVersion: 'v1',
    kind: 'Service',
    metadata: {
      name: 'jaeger-query',
      namespace: jaeger.namespace,
      labels: { 'app.kubernetes.io/name': jaeger.deployment.metadata.name },
    },
    spec: {
      ports: [
        { name: 'query', targetPort: 16686, port: 16686 },
      ],
      selector: jaeger.deployment.metadata.labels,
    },
  },

  adminService: {
    apiVersion: 'v1',
    kind: 'Service',
    metadata: {
      name: 'jaeger-admin',
      namespace: jaeger.namespace,
      labels: { 'app.kubernetes.io/name': jaeger.deployment.metadata.name },
    },
    spec: {
      ports: [
        { name: 'admin-http', targetPort: 14269, port: 14269 },
      ],
      selector: jaeger.deployment.metadata.labels,
    },
  },

  serviceAccount: {
    apiVersion: 'v1',
    kind: 'ServiceAccount',
    metadata: {
      name: 'jaeger',
      namespace: jaeger.namespace,
    },
  },

  role: {
    apiVersion: 'rbac.authorization.k8s.io/v1',
    kind: 'Role',
    metadata: {
      name: 'jaeger',
      namespace: jaeger.namespace,
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

  roleBinding: {
    apiVersion: 'rbac.authorization.k8s.io/v1',
    kind: 'RoleBinding',
    metadata: {
      name: 'jaeger',
      namespace: jaeger.namespace,
    },
    roleRef: {
      apiGroup: 'rbac.authorization.k8s.io',
      kind: 'Role',
      name: jaeger.role.metadata.name,
    },
    subjects: [
      {
        kind: 'ServiceAccount',
        name: jaeger.serviceAccount.metadata.name,
      },
    ],
  },

  deployment: {
    local labels = { 'app.kubernetes.io/name': jaeger.deployment.metadata.name },
    apiVersion: 'apps/v1',
    kind: 'Deployment',
    metadata: {
      name: 'jaeger-all-in-one',
      namespace: jaeger.namespace,
      labels: labels,
    },
    spec: {
      replicas: jaeger.replicas,
      selector: { matchLabels: jaeger.deployment.metadata.labels },
      strategy: {
        rollingUpdate: {
          maxSurge: 0,
          maxUnavailable: 1,
        },
      },
      template: {
        metadata: {
          labels: labels,
        },
        spec: {
          serviceAccountName: jaeger.serviceAccount.metadata.name,
          containers: [{
            name: jaeger.deployment.metadata.name,
            image: jaeger.image,
            args: ['--collector.queue-size=4000'] + (if jaeger.queryBasePath != '' then ['--query.base-path=' + jaeger.queryBasePath] else []),
            env: [{
              name: 'SPAN_STORAGE_TYPE',
              value: 'memory',
            }],
            securityContext: {
              runAsUser: 65534,
            },
            ports: [
              { name: 'admin-http', containerPort: 14269 },
              { name: 'query', containerPort: 16686 },
              { name: 'grpc', containerPort: 14250 },
            ],
            livenessProbe: { failureThreshold: 4, periodSeconds: 30, httpGet: {
              scheme: 'HTTP',
              port: 14269,
              path: '/',
            } },
            readinessProbe: { failureThreshold: 3, periodSeconds: 30, initialDelaySeconds: 10, httpGet: {
              scheme: 'HTTP',
              port: 14269,
              path: '/',
            } },
            resources: {
              requests: { cpu: '100m', memory: '1Gi' },
              limits: { cpu: '4', memory: '4Gi' },
            },
          }],
        },
      },
    },
  },
}
