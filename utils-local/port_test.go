package utils

import (
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
	"testing"
)

var content = `[{"name":"top1","ports":["80","443","8080"],"tags":["http"]},{"name":"top2","ports":["70","80-90","442-444","1080","2000-2001","3000-3001","1443","4443","4430","5000-5001","5601","6000-6003","7000-7003","9000-9003","8080-8091","8000-8020","8820","6443","8443","9443","8787","7080","8070","7070","7443","9080-9083","5555","6666","7777","7788","9999","6868","8888","8878","8889","7890","5678","6789","9090-9100","9988","9876","8765","8091","8099","8763","8848","8161","8060","8899","8088","800","801","888","10000-10010","1080-1082","10080","10443","18080","18000","18088","18090","19090-19091","50070"],"tags":["http","common"]},{"name":"top3","ports":["9443","6080","6443","9070","9092-9093","7003-7011","9003-9011","8100-8111","8161","8021-8030","8880-8890","8010-8020","8090-8100","8180-8181","8983","1311","8363","8800","8761","8873","8866","8900","8282","8999","8989","8066","8200","8111","8030","8040","8060","8180","10800","18081"],"tags":["http"]},{"name":"socks","ports":["1080"]},{"name":"iis","ports":["47001"],"tags":["http"]},{"name":"jboss","ports":["45566","4446","3873","5001"],"tags":["rce"]},{"name":"postgresql","ports":["5432"],"tags":["db","common","brute"]},{"name":"mssql","ports":["1433-1435","mssqlntlm"],"tags":["db","common","brute"]},{"name":"mysql","ports":["3306-3308","33060","33066"],"tags":["db","common","brute"]},{"name":"oracle","ports":["1158","1521","11521","210"],"tags":["db","common","in"]},{"name":"counchdb","ports":["5984","6984"],"tags":["db"]},{"name":"couchbase","ports":["8091","11210"],"tags":["db"]},{"name":"influxDB","ports":["8086"],"tags":["db"]},{"name":"hdfs","ports":["8020","50010"],"tags":["db"]},{"name":"clickhouse","ports":["8123"],"tags":["db"]},{"name":"redis","ports":["6379"],"tags":["db","common","rce","in","brute"]},{"name":"memcache","ports":["11211"],"tags":["db","in","common","brute"]},{"name":"dm(达梦)","ports":["5236"],"tags":["db","in"]},{"name":"oscar(神通)","ports":["2003"],"tags":["db","in"]},{"name":"sybase","ports":["5000","4100"],"tags":["db"]},{"name":"mongodb","ports":["27017-27019"],"tags":["db","common","brute"]},{"name":"hbase","ports":["16000","16010","16201"],"tags":["db"]},{"name":"cassandra","ports":["9042","7000"],"tags":["db"]},{"name":"rabbitmq","ports":["15672","5672"],"tags":["db","common"]},{"name":"neo4j","ports":["7474","7687"],"tags":["db"]},{"name":"hessian","ports":["7848"],"tags":["rce"]},{"name":"jndi","ports":["1098-1101","1000-1001","4444-4447","10999","19001","9999","8083","8686","10001","11099","5001"],"tags":["rce","common","in"]},{"name":"jdwp","ports":["5005","8453"],"tags":["rce","common","in"]},{"name":"websphere","ports":["8880","2809","9100","11006"],"tags":["http","rce"]},{"name":"jmx","ports":["8686","8093","9010-9012","50500","61616"],"tags":["rce","common","in"]},{"name":"php-xdebug","ports":["9000"],"tags":["rce","in"]},{"name":"nodejs-debug","ports":["5858","9229"],"tags":["rce"]},{"name":"glassfish","ports":["4848"],"tags":["rce"]},{"name":"rocketmq","ports":["9876","10909","10911","10912"],"tags":["rce","common","in"]},{"name":"activemq","ports":["8161","61616"],"tags":["rce","common","in"]},{"name":"kafka","ports":["9092"],"tags":["rce"]},{"name":"cisco","ports":["4786"],"tags":["rce"]},{"name":"rlogin","ports":["512-514"],"tags":["rce"]},{"name":"hp","ports":["5555","5556"],"tags":["rce"]},{"name":"etcd","ports":["2379","2380"],"tags":["common","in","cloud"]},{"name":"istio","ports":["15010","15011","15012"],"tags":["common","in","cloud"]},{"name":"envoy","ports":["15001"],"tags":["common","in","cloud"]},{"name":"jaeger","ports":["14268","16686"],"tags":["common","in","cloud"]},{"name":"k8s","ports":["10256","10250","10255"],"tags":["common","in","cloud"]},{"name":"kafka","ports":["9092"],"tags":["common","in","cloud"]},{"name":"nats","ports":["4222","8222"],"tags":["common","in","cloud"]},{"name":"pulsar","ports":["6650"],"tags":["common","in","cloud"]},{"name":"consul","ports":["8500","8300-8302"],"tags":["common","in","cloud"]},{"name":"nacos","ports":["8848-8850","9700"],"tags":["common","in","cloud"]},{"name":"docker","ports":["2375-2378"],"tags":["rce","common","in","cloud"]},{"name":"portainer","ports":["9000"],"tags":["rce","in"]},{"name":"ajp","ports":["8009"],"tags":["rce","common","in"]},{"name":"elasticsearch","ports":["9200","9300"],"tags":["rce","in","db","brute","common"]},{"name":"windows","ports":["icmp","22","135","137","445","3389","winrm","oxid"],"tags":["win","common","in"]},{"name":"telnet","ports":["23"],"tags":["win","common","in"]},{"name":"ldap","ports":["389"],"tags":["win","db","common","in"]},{"name":"kerberos","ports":["88"],"tags":["win","common","in"]},{"name":"snmp","ports":["161"],"tags":["win","brute"]},{"name":"ping","ports":["icmp"],"tags":["win"]},{"name":"ftp","ports":["21","2121"],"tags":["win","common","brute"]},{"name":"other","ports":["21-23","69","161","901-902","50000"],"tags":["info","in"]},{"name":"smtp","ports":["25","587","465","2525"],"tags":["mail"]},{"name":"pop3","ports":["110","995"],"tags":["mail"]},{"name":"imap","ports":["143","993"],"tags":["mail"]},{"name":"zookeeper","ports":["2181","2888","3888"],"tags":["info","common","in"]},{"name":"rsync","ports":["873"],"tags":["info","brute","common","in"]},{"name":"lotus","ports":["1352"],"tags":["info","in"]},{"name":"nfs","ports":["2049"],"tags":["rce","in"]},{"name":"oracle-ftp","ports":["2100"]},{"name":"squid","ports":["3128"],"tags":["rce"]},{"name":"pcanywhere","ports":["5632"],"tags":["info"]},{"name":"ssh","ports":["22","2222","10022"],"tags":["info","common","in"]},{"name":"vnc","ports":["5900","5901","5800"],"tags":["brute","common","in","rce"]},{"name":"hadoop","ports":["8088","50070","50010","50020"],"tags":["info"]},{"name":"vmware","ports":["9875","427"],"tags":["in","common","rce"]},{"name":"kibana","ports":["5601"],"tags":["info","common"]},{"name":"rdp","ports":["3389","13389","33899","33389"],"tags":["win","common","brute"]},{"name":"dubbo","ports":["18086","20880-20882"],"tags":["common","rce"]},{"name":"深信服ssl-vpn","ports":["9990","4430","8870"],"tags":["rce","common"]},{"name":"adb","ports":["5555"],"tags":["rce","common"]}]`

func TestLoadPort(t *testing.T) {
	var ports []*PortConfig
	err := yaml.Unmarshal([]byte(content), &ports)
	if err != nil {
		t.Fatal(err)
	}

	preset := NewPortPreset(ports)
	assert.NotNil(t, preset)
	assert.Equal(t, len(preset.NameMap), len(ports))
}

func TestChoicePort(t *testing.T) {
	var ports []*PortConfig
	err := yaml.Unmarshal([]byte(content), &ports)
	if err != nil {
		t.Fatal(err)
	}
	preset := NewPortPreset(ports)

	expectedPorts := []string{"22", "135", "137", "445", "3389", "winrm", "oxid"}
	actualPorts := preset.ChoicePort("win")
	assert.ElementsMatch(t, expectedPorts, actualPorts)
}

func TestParsePortString(t *testing.T) {
	var ports []*PortConfig
	err := yaml.Unmarshal([]byte(content), &ports)
	if err != nil {
		t.Fatal(err)
	}
	preset := NewPortPreset(ports)

	testCases := []struct {
		input    string
		expected []string
	}{
		{
			input:    "top2,-win,-84,-1-10000",
			expected: []string{"10001", "10002", "10003", "10004", "10005", "10006", "10007", "10008", "10009", "10010", "10080", "10443", "18080", "18000", "18088", "18090", "19090", "19091", "50070"},
		},
		{
			input:    "all,-1-10000",
			expected: []string{"19090", "61616", "33899", "10006", "10443", "18000", "20882", "10005", "33066", "10911", "11211", "27018", "11210", "18081", "10007", "50010", "10255", "10010", "45566", "15010", "11006", "10080", "19001", "11099", "18080", "19091", "15011", "15001", "50000", "33389", "oxid", "50070", "10250", "18088", "27017", "50500", "47001", "14268", "winrm", "10008", "10022", "15672", "10800", "16686", "10004", "10909", "33060", "16000", "16201", "10999", "13389", "10009", "mssqlntlm", "27019", "20880", "icmp", "10001", "18090", "20881", "50020", "10003", "11521", "10912", "10256", "16010", "18086", "10002", "15012"},
		},
		//{
		//	input:    "-",
		//  expected: []string{},
		//},
	}

	for _, tc := range testCases {
		actual := preset.ParsePortString(tc.input)
		assert.ElementsMatch(t, tc.expected, actual)
	}
}
