# -*- mode: ruby -*-
# vi: set ft=ruby :

LIBVIRT_HOST_IP = ENV["LIBVIRT_HOST_IP"] || "192.168.56.1"
HECKLER_IP = ENV["HECKLER_IP"] || "192.168.56.4"
BRIDGE_IF = ENV["BRIDGE_IF"] || false

Vagrant.configure("2") do |config|
  config.vm.provider :libvirt do |libvirt|
    libvirt.qemu_use_session = false
  end

  config.vm.define "heckler" do |heckler|
    heckler.vm.box = "generic/ubuntu2004"
    heckler.vm.synced_folder "./", "/heckler/"

    if BRIDGE_IF
      heckler.vm.network "public_network", ip: HECKLER_IP,
                                               bridge: BRIDGE_IF,
                                               libvirt__network_name: "heckler_network",
                                               libvirt__host_ip: LIBVIRT_HOST_IP,
                                               libvirt__netmask: "255.255.255.0",
                                               libvirt__dhcp_enabled: false,
                                               auto_config: false
    else
      heckler.vm.network "private_network", ip: HECKLER_IP,
                                                libvirt__network_name: "heckler_network",
                                                libvirt__host_ip: LIBVIRT_HOST_IP,
                                                libvirt__netmask: "255.255.255.0",
                                                libvirt__dhcp_enabled: false,
                                                auto_config: false
    end

    heckler.vm.provider "virtualbox" do |v, override|
      v.memory = 2048
      v.cpus = 2
      override.vm.synced_folder "./", "/heckler/"
    end

    heckler.vm.provider "libvirt" do |l, override|
      l.memory = 2048
      l.cpus = 2
      override.vm.synced_folder "./", "/heckler/"
    end

    heckler.vm.provision :shell, path: "setup.sh", args: [HECKLER_IP]
  end
end
