class fozzie {
  file { "/data/puppet_apply/fozzie":
    ensure => directory,
  }
  file { "/data/puppet_apply/fozzie/slapstick":
    ensure => absent,
  }
  service { 'nginx':
    ensure => stopped,
  }
  file { "/data/puppet_apply/fozzie/styx":
    ensure => present,
    content => "",
  }
}
