class waldorf {
  include muppetshow

  file { "/data/puppet_apply/waldorf":
    ensure => directory,
  }
  file { "/data/puppet_apply/waldorf/poignant":
    ensure => present,
    content => "acerbic\n",
  }
}
