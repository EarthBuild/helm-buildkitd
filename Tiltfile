release_name = 'buildkitd-stack-dev'

templated = helm(
    './helm/buildkitd-stack',
    name=release_name
)

k8s_yaml(templated)
