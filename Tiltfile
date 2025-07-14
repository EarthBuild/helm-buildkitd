release_name = 'buildkitd-stack-dev'

load('ext://earthly', 'earthly_build')

local_img_ref = 'local/autoscaler'
earthly_build(
    context='.',
    target='+tilt-image',
    ref=local_img_ref,
    image_arg='IMG_NAME',
    extra_flags=['--strict'],
)

templated = helm(
    './helm/buildkitd-stack',
    name=release_name,
    set=[
        # hostpath works for docker desktop k8s, set this appropriately for your development k8s cluster
        'buildkitd.persistence.storageClassName=hostpath',
        # use the version of the proxy image we've built locally
        'autoscaler.image.repository={}'.format(local_img_ref)
    ]
)

k8s_yaml(templated)
