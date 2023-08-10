define muppetshow::episode(
  String[1] $base,
){
  file { "${base}/laughtrack":
    ensure => present,
    content => "Wacka\nWacka\n",
  }
}
