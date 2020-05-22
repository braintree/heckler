class fozzie {
  file { "/data/puppet_apply/fozzie":
    ensure => directory,
  }
  file { "/data/puppet_apply/fozzie/slapstick":
    ensure => present,
    content => "wacka wacka!\n",
  }
  file { "/data/puppet_apply/fozzie/styx":
    ensure => present,
    content => "",
  }
}
