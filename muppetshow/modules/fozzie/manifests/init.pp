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
  file { "/data/puppet_apply/fozzie/manhattan":
    ensure => present,
    content => "Let's take manhattan!\n",
  }
  file { "/var/www/html/index.html":
    ensure => present,
    content => "Fozzie\n",
  }
  package { 'bsdgames':
    ensure => installed,
  }
  file { "/data/puppet_apply/fozzie/styx":
    ensure => present,
    content => "Come Sail Away\n",
  }
  exec { 'sail':
    command => '/usr/games/sail -s',
    refreshonly => true,
    subscribe => File["/data/puppet_apply/fozzie/styx"],
  }
}
