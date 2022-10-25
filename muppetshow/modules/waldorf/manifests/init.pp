class waldorf {
  include muppetshow

  file { "/data/puppet_apply/waldorf":
    ensure => directory,
  }
  file { "/data/puppet_apply/waldorf/poignant":
    ensure => present,
    content => "sour\n",
  }
  file { "/data/puppet_apply/waldorf/manhattan":
    ensure => present,
    content => "Gonzo\n",
  }
  service { 'nginx':
    ensure => running,
  }
  file { "/var/www/html/index.html":
    ensure => present,
    content => "Waldorf\n",
    notify => Service['nginx'],
  }
  package { 'sl':
    ensure => installed,
  }
}
