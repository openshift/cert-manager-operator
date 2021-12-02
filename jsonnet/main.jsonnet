// returns labels which are not 'helm.sh/chart' and don't have a 'helm' value (case insensitive)
local filterHelmLabels(labels) = {
  [k]: labels[k]
  for k in std.objectFields(labels)
  if std.asciiLower(labels[k]) != 'helm' && k != 'helm.sh/chart'
};

// removes helm labels from metadata.labels if labels exist.
// applies to all resource types
local cleanupHelmLabels(manifest) = manifest {
  metadata+: {
    [if 'labels' in manifest.metadata then 'labels']: filterHelmLabels(super.labels),
  },
};

// adds a command based on labels of the cert-manager container.
// removes helm labels in template metadata.
// applies only to deployments.
local cleanupDeployments(manifest) =
  if manifest.kind != 'Deployment'
  then manifest
  else manifest {
    spec+: {
      template+: {
        metadata+: {
          labels: filterHelmLabels(super.labels),
        },
        spec+: {
          containers: [
            c {
              local cmd = '/usr/bin/' + manifest.metadata.labels['app.kubernetes.io/component'],
              [if c.name == 'cert-manager' then 'command']: [cmd],
            }
            for c in super.containers
          ],
        },
      },
    },
  };

local suffix = {
  CustomResourceDefinition: 'crd',
  Namespace: 'namespace',
  ClusterRole: 'cr',
  ClusterRoleBinding: 'crb',
  RoleBinding: 'rb',
  Role: 'role',
  ServiceAccount: 'sa',
  MutatingWebhookConfiguration: 'mutatingwebhookconfiguration',
  ValidatingWebhookConfiguration: 'validatingwebhookconfiguration',
  Service: 'svc',
  Deployment: 'deployment',
};

// create a path including the file name based on the item.
local path(item) =
  // CRDs go into cert-manager-crds directory
  if item.kind == 'CustomResourceDefinition' then 'cert-manager-crds/' + item.metadata.name + '-' + suffix[item.kind] + '.yaml'
  // everything that has a component label goes into its own subdirectory
  else if 'labels' in item.metadata &&
          'app.kubernetes.io/component' in item.metadata.labels
  then 'cert-manager-deployment/' + item.metadata.labels['app.kubernetes.io/component'] + '/' + item.metadata.name + '-' + suffix[item.kind] + '.yaml'
  // else, leave it at the top-level
  else 'cert-manager-deployment/' + item.metadata.name + '-' + suffix[item.kind] + '.yaml';

// top level function (aka 'main')
function(manifest) {
  [std.strReplace(path(item), ':', '-')]: cleanupDeployments(cleanupHelmLabels(item))
  for item in manifest
}
