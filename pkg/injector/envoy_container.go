package injector

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/openservicemesh/osm/pkg/configurator"
	"github.com/openservicemesh/osm/pkg/constants"
)

const (
	envoyBootstrapConfigFile = "bootstrap.yaml"
	envoyProxyConfigPath     = "/etc/envoy"
)

func getPlatformSpecificSpecComponents(cfg configurator.Configurator, podOS string) (podSecurityContext *corev1.SecurityContext, envoyContainer string) {
	if strings.EqualFold(podOS, constants.OSWindows) {
		podSecurityContext = &corev1.SecurityContext{
			WindowsOptions: &corev1.WindowsSecurityContextOptions{
				RunAsUserName: func() *string {
					userName := constants.EnvoyWindowsUser
					return &userName
				}(),
			},
		}
		envoyContainer = cfg.GetEnvoyWindowsImage()
	} else {
		podSecurityContext = &corev1.SecurityContext{
			RunAsUser: func() *int64 {
				uid := constants.EnvoyUID
				return &uid
			}(),
		}
		envoyContainer = cfg.GetEnvoyImage()
	}
	return
}

func getEnvoySidecarContainerSpec(pod *corev1.Pod, cfg configurator.Configurator, originalHealthProbes healthProbes, podOS string) corev1.Container {
	// cluster ID will be used as an identifier to the tracing sink
	clusterID := fmt.Sprintf("%s.%s", pod.Spec.ServiceAccountName, pod.Namespace)
	securityContext, containerImage := getPlatformSpecificSpecComponents(cfg, podOS)

	return corev1.Container{
		Name:            constants.EnvoyContainerName,
		Image:           containerImage,
		ImagePullPolicy: corev1.PullAlways,
		SecurityContext: securityContext,
		Ports:           getEnvoyContainerPorts(originalHealthProbes),
		VolumeMounts: []corev1.VolumeMount{{
			Name:      envoyBootstrapConfigVolume,
			ReadOnly:  true,
			MountPath: envoyProxyConfigPath,
		}},
		Command:   []string{"envoy"},
		Resources: cfg.GetProxyResources(),
		Args: []string{
			"--log-level", cfg.GetEnvoyLogLevel(),
			"--config-path", strings.Join([]string{envoyProxyConfigPath, envoyBootstrapConfigFile}, "/"),
			"--service-cluster", clusterID,
			"--bootstrap-version 3",
		},
		Env: []corev1.EnvVar{
			{
				Name: "POD_UID",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.uid",
					},
				},
			},
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			},
			{
				Name: "POD_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "status.podIP",
					},
				},
			},
			{
				Name: "SERVICE_ACCOUNT",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "spec.serviceAccountName",
					},
				},
			},
		},
	}
}

func getEnvoyContainerPorts(originalHealthProbes healthProbes) []corev1.ContainerPort {
	containerPorts := []corev1.ContainerPort{
		{
			Name:          constants.EnvoyAdminPortName,
			ContainerPort: constants.EnvoyAdminPort,
		},
		{
			Name:          constants.EnvoyInboundListenerPortName,
			ContainerPort: constants.EnvoyInboundListenerPort,
		},
		{
			Name:          constants.EnvoyInboundPrometheusListenerPortName,
			ContainerPort: constants.EnvoyPrometheusInboundListenerPort,
		},
	}

	if originalHealthProbes.liveness != nil {
		livenessPort := corev1.ContainerPort{
			// Name must be no more than 15 characters
			Name:          "liveness-port",
			ContainerPort: livenessProbePort,
		}
		containerPorts = append(containerPorts, livenessPort)
	}

	if originalHealthProbes.readiness != nil {
		readinessPort := corev1.ContainerPort{
			// Name must be no more than 15 characters
			Name:          "readiness-port",
			ContainerPort: readinessProbePort,
		}
		containerPorts = append(containerPorts, readinessPort)
	}

	if originalHealthProbes.startup != nil {
		startupPort := corev1.ContainerPort{
			// Name must be no more than 15 characters
			Name:          "startup-port",
			ContainerPort: startupProbePort,
		}
		containerPorts = append(containerPorts, startupPort)
	}

	return containerPorts
}
