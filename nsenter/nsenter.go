package nsenter

/*
#define _GNU_SOURCE
#include <unistd.h>
#include <errno.h>
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
//只要这个包被导入 它就会在所有 Go 代码前执行,故使用环境变量
__attribute__((constructor)) void enter_namespace(void) {
	char *mydocker_pid;
	mydocker_pid = getenv("mydocker_pid");
	if (mydocker_pid) {
		//fprintf(stdout, "got mydocker_pid=%s\n", mydocker_pid);
	} else {
		//fprintf(stdout, "missing mydocker_pid env skip nsenter");
		return;
	}
	char *mydocker_cmd;
	mydocker_cmd = getenv("mydocker_cmd");
	if (mydocker_cmd) {
		//fprintf(stdout, "got mydocker_cmd=%s\n", mydocker_cmd);
	} else {
		//fprintf(stdout, "missing mydocker_cmd env skip nsenter");
		return;
	}
	int i;
	char nspath[1024];
	char *namespaces[] = { "ipc", "uts", "net", "pid", "mnt" };

	for (i=0; i<5; i++) {
		sprintf(nspath, "/proc/%s/ns/%s", mydocker_pid, namespaces[i]);
		int fd = open(nspath, O_RDONLY);

		//setns 个系统调用，可以根据提供的 PID 再次进入到指定的 Namespace 。
		//它需要先打开／proc/[pid]/ns／文件夹下对应的文件，然后使当前进程进入到指定的 Namespace 。
		//系统调用描述非常简单，但是有一点对于 Go 来说很麻烦。对于 Mount Namespace 来说， 一个具有多线程的进程是无法使用 setns 调用进入到对应的命名空间的。
		//但是， Go 每启动一个程序就会进入多线程状态，因此无法简简单单地在 Go 里面直接调用系统调用，使当前的进程进入对应Mount Namespace 。这里需要借助C来实现这个功能
		// 参考： https://www.cnblogs.com/YaoDD/p/6225803.html
		if (setns(fd, 0) == -1) {
			//fprintf(stderr, "setns on %s namespace failed: %s\n", namespaces[i], strerror(errno));
		} else {
			//fprintf(stdout, "setns on %s namespace succeeded\n", namespaces[i]);
		}
		close(fd);
	}
	int res = system(mydocker_cmd);
	exit(0);
	return;
}
*/
import "C"
