/**
 *    Licensed under the Apache License, Version 2.0 (the "License");
 *    you may not use this file except in compliance with the License.
 *    You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 *    Unless required by applicable law or agreed to in writing, software
 *    distributed under the License is distributed on an "AS IS" BASIS,
 *    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *    See the License for the specific language governing permissions and
 *    limitations under the License.
 *
 *    @copyright 2014 Safehaus.org
 */
/**
 *  @brief     SubutaiEnvironment.h
 *  @class     SubutaiEnvironment.h
 *  @details   SubutaiEnvironment Class is designed for getting and setting environment variables and special informations.
 *  		   This class's instance can get get useful Agent's specific Environment informations
 *  		   such us IPs, UUID, hostname, macID, parentHostname, etc..
 *  @author    Emin INAL
 *  @author    Bilal BAL
 *  @version   1.1.0
 *  @date      Sep 13, 2014
 */
#ifndef SUBUTAIENVIRONMENT_H_
#define SUBUTAIENVIRONMENT_H_
#include <syslog.h>
#include <iostream>
#include <fstream>
#include <string>
#include <vector>
#include <cstdlib>
#include <sstream>
#include <list>
#include "pugixml.hpp"
#include <boost/uuid/uuid.hpp>
#include <boost/uuid/uuid_generators.hpp>
#include <boost/uuid/uuid_io.hpp>
#include <boost/lexical_cast.hpp>
#include <boost/thread/thread.hpp>
#include <boost/property_tree/ptree.hpp>
#include <boost/property_tree/ini_parser.hpp>
#include "SubutaiLogger.h"
using namespace std;
using std::stringstream;
using std::string;

class SubutaiContainer
{
public:
	SubutaiContainer( SubutaiLogger*, lxc_container* cont );
	virtual ~SubutaiContainer( void );
	string toString( int );
	int getContainerSettings();
	bool getContainerUuid();
	bool getContainerMacAddress();
	bool getContainerHostname();
	bool getContainerParentHostname();
	bool getContainerIpAddress();
	string getContainerUuidValue();
	string getContainerHostnameValue();
	string getContainerMacAddressValue();
	string getContainerParentHostnameValue();
	string getContainerConnectionUrlValue();
	string getContainerConnectionPortValue();
	string getContainerConnectionOptionsValue();
	vector<string> getContainerIpValue();
	string RunProgram(string program, vector<string> params)

private:
	lxc_container* container;
	string uuid;
	string macAddress;
	string hostname;
	string parentHostname;
	vector<string> ipAddress;
	SubutaiLogger*	environmentLogger;
};
#endif /* SUBUTAIENVIRONMENT_H_ */


