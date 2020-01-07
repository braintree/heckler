File {
  backup => false,
}

node default {
  include $facts['nodename']
}


class fozzie {
  include muppetshow

  file { "${facts['cwd']}/nodes/fozzie":
    ensure => directory,
  }
  file { "${facts['cwd']}/nodes/fozzie/slapstick":
    ensure => present,
    content => "wacka wacka!\n",
  }
  file { "${facts['cwd']}/nodes/fozzie/styx":
    ensure => present,
    content => "",
  }
}

class statler {
  include muppetshow

  file { "${facts['cwd']}/nodes/statler":
    ensure => directory,
  }

  file { "${facts['cwd']}/nodes/statler/wit":
    ensure => present,
    content => "terrible\n",
  }
}

class waldorf {
  include muppetshow

  file { "${facts['cwd']}/nodes/waldorf":
    ensure => directory,
  }
  file { "${facts['cwd']}/nodes/waldorf/poignant":
    ensure => present,
    content => "acerbic\n",
  }
}
